<script lang="ts">
  // Issue Detail — per-issue audit timeline, agent stream, validation results,
  // memory entries, knowledge context. SPEC §18.4 (2). Wired up in Phase 15.
  import { page } from '$app/stores';
  const issueId = $derived($page.params.id);
</script>

<h2 class="text-2xl font-semibold">Issue {issueId}</h2>
<p class="mt-2 text-sm text-zinc-400">Detail view placeholder.</p>
