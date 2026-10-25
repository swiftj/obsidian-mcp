// Probe dump for the scratch vault ONLY (docs/probes/CHECKLIST.md).
// Paste into Obsidian's developer console (Cmd+Opt+I → Console).
// Read-only except for one file it writes: probe-dump-<label>.json in the vault root.
// Link-format settings are changed only in memory, and restored before it finishes.
(async (label) => {
	const out = { label, userAgent: navigator.userAgent, errors: {} };
	const plain = (v, depth = 0) => {
		if (v === null || typeof v !== 'object') return typeof v === 'function' ? undefined : v;
		if (v.path !== undefined && v.vault !== undefined) return { $file: v.path };
		if (depth > 8) return '$depth';
		if (Array.isArray(v)) return v.map((x) => plain(x, depth + 1));
		const o = {};
		for (const [k, x] of Object.entries(v)) {
			if (k === 'app' || k === 'vault' || k === 'parent') continue;
			const p = plain(x, depth + 1);
			if (p !== undefined) o[k] = p;
		}
		return o;
	};
	const tryit = async (name, fn) => {
		try { out[name] = plain(await fn()); } catch (e) { out.errors[name] = String(e); }
	};
	const file = (p) => app.vault.getAbstractFileByPath(p);
	if (!file('pr01/src.md')) {
		console.error(`this window's vault is "${app.vault.getName()}", not ObsProbe; aborting (nothing written)`);
		return;
	}

	const cfgKeys = ['newLinkFormat', 'useMarkdownLinks', 'alwaysUpdateLinks', 'newFileLocation',
		'newFileFolderPath', 'attachmentFolderPath', 'userIgnoreFilters', 'trashOption',
		'promptDelete', 'showUnsupportedFiles', 'strictLineBreaks', 'propertiesInDocument'];
	await tryit('config', () => Object.fromEntries(cfgKeys.map((k) => [k, app.vault.getConfig(k)])));
	await tryit('rawConfig', () => app.vault.config);
	await tryit('configDir', () => app.vault.configDir);

	await tryit('fileCache', () => Object.fromEntries(
		app.vault.getMarkdownFiles().map((f) => [f.path, app.metadataCache.getFileCache(f)])));
	await tryit('resolvedLinks', () => app.metadataCache.resolvedLinks);
	await tryit('unresolvedLinks', () => app.metadataCache.unresolvedLinks);
	await tryit('tags', () => app.metadataCache.getTags());

	const q = [
		['Tie', 'pr01/src.md'], ['Tie', 'pr01/a/src-a.md'], ['Tie', 'pr01/b/src-b.md'],
		['Tie', 'pr01/c/src-c.md'], ['Tie', 'pr01/a/deep/src-a-deep.md'], ['Tie', 'pr01-root-src.md'],
		['a/Tie', 'pr01/src.md'], ['b/Tie', 'pr01/src.md'], ['a/Tie', 'pr01-root-src.md'],
		['pr01/a/Tie', 'pr01-root-src.md'], ['Tie.md', 'pr01/src.md'],
		['Near', 'pr01/src.md'], ['Near', 'pr01/c/src-c.md'], ['Near', 'pr01/a/src-a.md'], ['Near', 'pr01-root-src.md'],
		['case target', 'pr02/src.md'], ['CASE TARGET', 'pr02/src.md'], ['case target.md', 'pr02/src.md'],
		['pr02/case target', 'pr02/src.md'], ['case%20target.md', 'pr02/src.md'],
		['Nick Name', 'pr03/src.md'], ['other alias', 'pr03/src.md'], ['Clash', 'pr03/src.md'],
		['../Md Target.md', 'pr10/sub/src.md'], ['../Md%20Target.md', 'pr10/sub/src.md'],
		['Md Target', 'pr10/sub/src.md'], ['./../Md Target.md', 'pr10/sub/src.md'],
		['/pr10/Md Target.md', 'pr10/sub/src.md'], ['pr10/Md Target.md', 'pr10/sub/src.md'],
		['../Md Target', 'pr10/sub/src.md'], ['Target14', 'pr14/in-normal.md'],
	];
	await tryit('resolve', () => q.map(([l, s]) => ({ link: l, source: s,
		dest: app.metadataCache.getFirstLinkpathDest(l, s)?.path ?? null })));

	// Link generation for every newLinkFormat × useMarkdownLinks combination (in memory only).
	const gen = [
		['pr01/a/Tie.md', 'pr01/src.md', ''], ['pr01/a/Tie.md', 'pr01/b/src-b.md', ''],
		['pr01/Near.md', 'pr01/c/src-c.md', ''], ['pr01/c/Near.md', 'pr01/src.md', ''],
		['pr10/Md Target.md', 'pr10/sub/src.md', ''], ['pr02/Case Target.md', 'README.md', ''],
		['pr06/img.png', 'pr06/src.md', ''], ['pr16/Existing.canvas', 'README.md', ''],
		['README.md', 'pr01/a/deep/src-a-deep.md', ''],
		['pr13/Blocks.md', 'pr13/src.md', '#^para-1'],
	];
	const heads = app.metadataCache.getFileCache(file('pr13/Heads.md'))?.headings ?? [];
	for (const h of heads) gen.push(['pr13/Heads.md', 'pr13/src.md', '#' + h.heading]);
	const cfg = app.vault.config;
	const saved = {};
	for (const k of ['newLinkFormat', 'useMarkdownLinks']) saved[k] = [k in cfg, cfg[k]];
	out.generated = [];
	try {
		for (const fmt of ['shortest', 'relative', 'absolute']) {
			for (const md of [false, true]) {
				cfg.newLinkFormat = fmt; cfg.useMarkdownLinks = md;
				const rows = gen.map(([t, s, sub]) => {
					try {
						return { target: t, source: s, subpath: sub,
							linktext: app.metadataCache.fileToLinktext(file(t), s, true),
							markdown: app.fileManager.generateMarkdownLink(file(t), s, sub) };
					} catch (e) { return { target: t, source: s, subpath: sub, error: String(e) }; }
				});
				out.generated.push({ newLinkFormat: fmt, useMarkdownLinks: md,
					effective: [app.vault.getConfig('newLinkFormat'), app.vault.getConfig('useMarkdownLinks')], rows });
			}
		}
	} finally {
		for (const [k, [had, v]] of Object.entries(saved)) { if (had) cfg[k] = v; else delete cfg[k]; }
	}

	await tryit('newFileParent', () => app.fileManager.getNewFileParent('pr06/src.md')?.path);
	await tryit('attachmentPath', () => app.fileManager.getAvailablePathForAttachment('probe.png', 'pr06/src.md'));
	await tryit('userIgnored', () => Object.fromEntries(['pr06/excluded/Hidden.md', 'pr06/secret-note.md', 'pr06/src.md']
		.map((p) => [p, app.metadataCache.isUserIgnored?.(p)])));

	const mtm = app.metadataTypeManager;
	await tryit('typeManagerKeys', () => mtm && Object.keys(mtm));
	await tryit('typeManagerTypes', () => mtm?.types);
	await tryit('typeManagerProperties', () => mtm?.properties);
	await tryit('allPropertyInfos', () => app.metadataCache.getAllPropertyInfos?.());
	await tryit('assignedWidgets', () => mtm && Object.fromEntries(
		Object.keys(mtm.properties ?? {}).map((n) => [n, mtm.getAssignedWidget?.(n) ?? mtm.getAssignedType?.(n) ?? null])));

	const ip = app.internalPlugins;
	await tryit('corePluginsEnabled', () => ip?.config);
	await tryit('dailyNotesOptions', () => ip?.plugins?.['daily-notes']?.instance?.options);
	await tryit('templatesOptions', () => ip?.plugins?.templates?.instance?.options);

	const name = `probe-dump-${label}.json`;
	await app.vault.adapter.write(name, JSON.stringify(out, null, 1));
	console.log(`wrote ${name}; errors:`, out.errors);
})(String(Date.now()));
