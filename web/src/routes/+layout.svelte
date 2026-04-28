<script lang="ts">
  import '../app.css';
  import { page } from '$app/stores';

  const nav = [
    { href: '/', label: 'Overview' },
    { href: '/memory', label: 'Memory' },
    { href: '/knowledge', label: 'Knowledge' },
    { href: '/harness', label: 'Harness' },
    { href: '/docs', label: 'Doc Stores' },
    { href: '/audit', label: 'Audit' },
    { href: '/settings', label: 'Settings' }
  ];

  let { children } = $props();
</script>

<div class="flex min-h-screen">
  <aside class="w-56 border-r border-zinc-800 p-4">
    <h1 class="mb-6 text-lg font-semibold tracking-tight">Conductor</h1>
    <nav class="flex flex-col gap-1 text-sm">
      {#each nav as item}
        <a
          href={item.href}
          class="rounded px-2 py-1 hover:bg-zinc-800"
          class:bg-zinc-800={$page.url.pathname === item.href}
        >
          {item.label}
        </a>
      {/each}
    </nav>
  </aside>
  <main class="flex-1 p-6">
    {@render children?.()}
  </main>
</div>
