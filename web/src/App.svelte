<script lang="ts">
  type Health = { revision: string; database_configured: boolean; database_ready: boolean };
  type Todo = { id: number; title: string; created_at: string };

  let health = $state<Health | null>(null);
  let todos = $state<Todo[]>([]);
  let title = $state('');
  let message = $state('Loading application status…');
  let saving = $state(false);

  async function refresh() {
    try {
      const healthResponse = await fetch('/healthz');
      health = await healthResponse.json();
      if (!health?.database_configured) {
        message = 'PostgreSQL is not configured yet. Set DATABASE_URL when you are ready.';
        todos = [];
        return;
      }
      const response = await fetch('/api/todos');
      if (!response.ok) throw new Error('todos: ' + response.status);
      const payload = await response.json();
      todos = payload.items ?? [];
      message = health.database_ready ? 'API and PostgreSQL are ready.' : 'PostgreSQL is configured but not ready.';
    } catch (error) {
      message = error instanceof Error ? error.message : 'Unable to load application status.';
    }
  }

  async function addTodo() {
    const value = title.trim();
    if (!value || saving) return;
    saving = true;
    try {
      const response = await fetch('/api/todos', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ title: value })
      });
      if (!response.ok) throw new Error(await response.text());
      title = '';
      await refresh();
    } catch (error) {
      message = error instanceof Error ? error.message : 'Unable to create todo.';
    } finally {
      saving = false;
    }
  }

  $effect(() => { void refresh(); });
</script>

<main>
  <section class="hero">
    <p class="eyebrow">Hexa.Build starter</p>
    <h1>Svelte + Go + PostgreSQL</h1>
    <p>{message}</p>
    <div class="status-grid">
      <div><span>Frontend</span><strong>Svelte 5</strong></div>
      <div><span>API revision</span><strong>{health?.revision ?? 'checking'}</strong></div>
      <div><span>Database</span><strong>{health?.database_ready ? 'ready' : health?.database_configured ? 'checking' : 'not configured'}</strong></div>
    </div>
  </section>

  <section class="panel">
    <h2>Starter persistence check</h2>
    <p>This tiny todo flow proves the browser → Go API → PostgreSQL path without defining your product model for you.</p>
    <form onsubmit={(event) => { event.preventDefault(); void addTodo(); }}>
      <input bind:value={title} placeholder="Add a todo" aria-label="Todo title" disabled={!health?.database_ready || saving} />
      <button type="submit" disabled={!health?.database_ready || saving || !title.trim()}>{saving ? 'Saving…' : 'Add'}</button>
    </form>
    {#if health?.database_ready}
      {#if todos.length === 0}
        <p class="empty">No rows yet. Add one to exercise PostgreSQL.</p>
      {:else}
        <ul>{#each todos as todo (todo.id)}<li>{todo.title}</li>{/each}</ul>
      {/if}
    {:else}
      <p class="empty">Configure PostgreSQL to enable the persistence example.</p>
    {/if}
  </section>
</main>
