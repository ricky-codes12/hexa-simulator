package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	apphttp "hexa-simulator/internal/httpapi"
	"hexa-simulator/internal/postgresstore"
)

// sensor-onboard registers the simulator's fleet in Hexa.Sensor through Sensor's public admin API
// (plan S5): it creates the device types that are missing, then applies the four CSV imports of
// the onboarding package, validating each first. With --sources it also creates the three
// sources the fleet sends to. Everything is idempotent.
//
//	SENSOR_PASSWORD=... hexa-simulator sensor-onboard --sensor http://127.0.0.1:19190 --code 123456
//
// The second factor is the signing-in administrator's: --code, --recovery-code, or on a Dev plane
// SENSOR_TOTP_SECRET (the account's authenticator secret, so an agent can run it unattended).
func sensorOnboard(args []string) int {
	fs := flag.NewFlagSet("sensor-onboard", flag.ExitOnError)
	sensorURL := fs.String("sensor", env("SENSOR_URL", "http://127.0.0.1:19190"), "Hexa.Sensor base URL")
	tenant := fs.String("tenant", env("SENSOR_TENANT", "smf"), "tenant slug")
	user := fs.String("user", env("SENSOR_USER", "admin"), "tenant administrator")
	code := fs.String("code", "", "authenticator code")
	recovery := fs.String("recovery-code", "", "unused recovery code")
	zipPath := fs.String("zip", "", "onboarding package to apply (default: the fleet in DATABASE_URL)")
	kind := fs.String("kind", "", "only this kind")
	estate := fs.String("estate", "", "only this estate")
	output := fs.String("output", "", "only this output")
	dryRun := fs.Bool("dry-run", false, "validate the imports without applying them")
	sources := fs.Bool("sources", false, "also create the sources the fleet sends to")
	broker := fs.String("mqtt-broker", env("SENSOR_MQTT_BROKER", "mqtt://127.0.0.1:19300"), "broker the mqtt-subscribe source reads (--sources)")
	brokerUser := fs.String("mqtt-user", env("SENSOR_MQTT_USER", "hexa-sensor"), "broker username of the mqtt-subscribe source (--sources); password from SENSOR_MQTT_PASSWORD")
	listen := fs.String("tcp-listen", env("SENSOR_TCP_LISTEN", ":19192"), "listen address of the teltonika-tcp source (--sources)")
	keyFile := fs.String("push-key-file", "", "where to write a newly generated http-push key, mode 0600 (--sources)")
	_ = fs.Parse(args)

	password := os.Getenv("SENSOR_PASSWORD")
	if password == "" {
		fmt.Fprintln(os.Stderr, "set SENSOR_PASSWORD")
		return 2
	}
	pkg, err := loadPackage(*zipPath, *kind, *estate, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "onboarding package:", err)
		return 1
	}
	s, err := newSensorClient(*sensorURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := s.signIn(*tenant, *user, password, *code, *recovery, os.Getenv("SENSOR_TOTP_SECRET")); err != nil {
		fmt.Fprintln(os.Stderr, "sign in:", err)
		return 1
	}
	fmt.Printf("signed in to %s as %s/%s\n", *sensorURL, *tenant, *user)

	if *sources {
		if err := s.ensureSources(*broker, *brokerUser, os.Getenv("SENSOR_MQTT_PASSWORD"), *listen, *keyFile); err != nil {
			fmt.Fprintln(os.Stderr, "sources:", err)
			return 1
		}
	}
	if err := s.ensureDeviceTypes(pkg["00_device_types.json"], *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "device types:", err)
		return 1
	}
	for _, f := range []struct{ name, kind string }{{"01_estates.csv", "estates"}, {"02_devices.csv", "devices"}, {"03_assets.csv", "assets"}, {"04_assignments.csv", "assignments"}} {
		if err := s.importCSV(f.kind, f.name, pkg[f.name], *dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f.name, err)
			return 1
		}
	}
	if *dryRun {
		fmt.Println("dry run: nothing was applied")
	} else {
		fmt.Println("onboarding applied; Sensor accepts the units within 30 s (its registry cache)")
	}
	return 0
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// loadPackage reads an onboarding ZIP from a file, or builds one from the fleet in DATABASE_URL.
func loadPackage(path, kind, estate, output string) (map[string][]byte, error) {
	var data []byte
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		data = b
	} else {
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return nil, errors.New("pass --zip or set DATABASE_URL to read the fleet")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := postgresstore.Open(ctx, dsn)
		if err != nil {
			return nil, err
		}
		defer store.Close()
		items, err := store.ListDevices(ctx)
		if err != nil {
			return nil, err
		}
		var picked []apphttp.Device
		for _, d := range items {
			if (kind == "" || d.Kind == kind) && (estate == "" || d.Estate == estate) && (output == "" || d.Output == output) {
				picked = append(picked, d)
			}
		}
		if len(picked) == 0 {
			return nil, errors.New("no devices match")
		}
		if data, err = apphttp.OnboardingZIP(picked); err != nil {
			return nil, err
		}
		fmt.Printf("package: %d devices\n", len(picked))
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[f.Name] = b
	}
	for _, name := range []string{"00_device_types.json", "01_estates.csv", "02_devices.csv", "03_assets.csv", "04_assignments.csv"} {
		if _, ok := out[name]; !ok {
			return nil, fmt.Errorf("the package has no %s", name)
		}
	}
	return out, nil
}

type sensorClient struct {
	base string
	http *http.Client
	csrf string
}

func newSensorClient(base string) (*sensorClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &sensorClient{base: strings.TrimRight(base, "/"), http: &http.Client{Jar: jar, Timeout: 60 * time.Second}}, nil
}

// call sends a JSON request and decodes the answer into out; a status outside ok is an error.
func (s *sensorClient) call(method, path string, body any, out any, ok ...int) (int, error) {
	var reader io.Reader
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case *multipartBody:
		reader, contentType = b.buf, b.contentType
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.base+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", s.base)
	if s.csrf != "" {
		req.Header.Set("X-CSRF-Token", s.csrf)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if len(ok) == 0 {
		ok = []int{200, 201, 204}
	}
	for _, code := range ok {
		if resp.StatusCode == code {
			if out != nil && len(raw) > 0 {
				if err := json.Unmarshal(raw, out); err != nil {
					return resp.StatusCode, fmt.Errorf("%s %s: %w", method, path, err)
				}
			}
			return resp.StatusCode, nil
		}
	}
	return resp.StatusCode, fmt.Errorf("%s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(raw[:min(len(raw), 600)])))
}

func (s *sensorClient) signIn(tenant, user, password, code, recovery, secret string) error {
	var login struct {
		Stage string `json:"stage"`
		CSRF  string `json:"csrf_token"`
	}
	if _, err := s.call("POST", "/api/v1/auth/login", map[string]string{"tenant": tenant, "username": user, "password": password}, &login); err != nil {
		return err
	}
	s.csrf = login.CSRF
	if login.Stage == "enrol" {
		return errors.New(user + " has no authenticator yet; enrol one in the console first")
	}
	second := map[string]string{}
	switch {
	case code != "":
		second["code"] = code
	case recovery != "":
		second["recovery_code"] = recovery
	case secret != "":
		c, err := totp(secret, time.Now())
		if err != nil {
			return err
		}
		second["code"] = c
	default:
		return errors.New("pass --code or --recovery-code (fresh MFA is needed for sources and keys)")
	}
	path := "/api/v1/auth/step-up"
	if login.Stage == "mfa" {
		path = "/api/v1/auth/mfa/verify"
	}
	_, err := s.call("POST", path, second, nil)
	return err
}

// totp is RFC 6238 SHA-1, 6 digits, 30 s.
func totp(secret string, now time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(strings.TrimSpace(secret), "=")))
	if err != nil {
		return "", fmt.Errorf("SENSOR_TOTP_SECRET: %w", err)
	}
	mac := hmac.New(sha1.New, key)
	_ = binary.Write(mac, binary.BigEndian, uint64(now.Unix()/30))
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000), nil
}

func (s *sensorClient) ensureDeviceTypes(raw []byte, dryRun bool) error {
	var types []map[string]any
	if err := json.Unmarshal(raw, &types); err != nil {
		return err
	}
	var existing struct {
		Items []struct {
			Key string `json:"key"`
		} `json:"items"`
	}
	if _, err := s.call("GET", "/api/v1/admin/device-profiles?limit=1000", nil, &existing); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, p := range existing.Items {
		have[p.Key] = true
	}
	for _, t := range types {
		key, _ := t["key"].(string)
		if have[key] {
			fmt.Printf("device type %s exists\n", key)
			continue
		}
		if dryRun {
			fmt.Printf("device type %s would be created\n", key)
			continue
		}
		if _, err := s.call("POST", "/api/v1/admin/device-profiles", t, nil); err != nil {
			return err
		}
		fmt.Printf("device type %s created\n", key)
	}
	return nil
}

type multipartBody struct {
	buf         *bytes.Buffer
	contentType string
}

type importReport struct {
	ImportID  string `json:"import_id"`
	RowsTotal int    `json:"rows_total"`
	RowsOK    int    `json:"rows_ok"`
	Errors    []struct {
		Row    int    `json:"row"`
		Column string `json:"column"`
		Reason string `json:"reason"`
	} `json:"errors"`
}

func (s *sensorClient) importCSV(kind, name string, data []byte, dryRun bool) error {
	modes := []string{"validate", "apply"}
	if dryRun {
		modes = modes[:1]
	}
	for _, mode := range modes {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("kind", kind)
		_ = w.WriteField("mode", mode)
		part, err := w.CreateFormFile("file", name)
		if err != nil {
			return err
		}
		if _, err := part.Write(data); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		var rep importReport
		if _, err := s.call("POST", "/api/v1/admin/imports", &multipartBody{&buf, w.FormDataContentType()}, &rep, 200, 201); err != nil {
			return err
		}
		if rep.RowsOK != rep.RowsTotal {
			var problems []string
			for _, e := range rep.Errors {
				problems = append(problems, fmt.Sprintf("row %d %s: %s", e.Row, e.Column, e.Reason))
			}
			return fmt.Errorf("%s: %d of %d rows ok: %s", mode, rep.RowsOK, rep.RowsTotal, strings.Join(problems[:min(len(problems), 8)], "; "))
		}
		fmt.Printf("%-19s %s: %d of %d rows ok\n", name, mode, rep.RowsOK, rep.RowsTotal)
	}
	return nil
}

type connector struct {
	ID        string `json:"instance_id"`
	Key       string `json:"key"`
	PluginKey string `json:"plugin_key"`
	Secrets   []struct {
		Key string `json:"key"`
	} `json:"secrets"`
}

// ensureSources creates the three sources the fleet sends to, with names free of demo wording.
func (s *sensorClient) ensureSources(broker, brokerUser, brokerPassword, listen, keyFile string) error {
	var list struct {
		Items []connector `json:"items"`
	}
	if _, err := s.call("GET", "/api/v1/admin/connectors", nil, &list); err != nil {
		return err
	}
	have := map[string]connector{}
	for _, c := range list.Items {
		have[c.Key] = c
	}
	want := []struct {
		key, name, plugin string
		settings          map[string]any
	}{
		{"estate-broker", "Estate broker", "mqtt-subscribe", map[string]any{"broker_url": broker, "topics": []string{"+/data"}, "username": brokerUser, "hardware_id_topic_level": 0}},
		{"fmc-direct", "FMC trackers direct", "teltonika-tcp", map[string]any{"listen": listen}},
		{"vendor-cloud", "Vendor cloud push", "http-push", map[string]any{}},
	}
	for _, w := range want {
		c, exists := have[w.key]
		if !exists {
			if _, err := s.call("POST", "/api/v1/admin/connectors", map[string]any{"key": w.key, "name": w.name, "plugin_key": w.plugin, "settings": w.settings}, &c); err != nil {
				return err
			}
			fmt.Printf("source %s (%s) created\n", w.key, w.plugin)
		} else {
			fmt.Printf("source %s exists\n", w.key)
		}
		switch w.plugin {
		case "mqtt-subscribe":
			if brokerPassword != "" {
				if _, err := s.call("PUT", "/api/v1/admin/connectors/"+url.PathEscape(w.key)+"/secret", map[string]string{"key": "password", "value": brokerPassword}, nil); err != nil {
					return err
				}
				fmt.Printf("source %s: broker password stored\n", w.key)
			}
		case "http-push":
			hasKey := false
			for _, sec := range c.Secrets {
				hasKey = hasKey || sec.Key == "ingest_key"
			}
			if hasKey {
				fmt.Printf("source %s already has an ingest key; keep using yours (rotate it on the source page)\n", w.key)
				continue
			}
			if keyFile == "" {
				return errors.New("pass --push-key-file so the new ingest key has somewhere to go")
			}
			var secret struct {
				Value    string `json:"value"`
				Endpoint string `json:"endpoint"`
			}
			if _, err := s.call("PUT", "/api/v1/admin/connectors/"+url.PathEscape(w.key)+"/secret", map[string]string{"key": "ingest_key"}, &secret); err != nil {
				return err
			}
			content := fmt.Sprintf("SIM_SENSOR_PUSH_URL=%s%s\nSIM_SENSOR_PUSH_KEY=%s\n", s.base, secret.Endpoint, secret.Value)
			if err := os.WriteFile(keyFile, []byte(content), 0o600); err != nil {
				return err
			}
			fmt.Printf("source %s: ingest key generated and written to %s\n", w.key, keyFile)
		}
	}
	return nil
}
