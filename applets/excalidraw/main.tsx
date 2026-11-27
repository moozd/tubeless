// excalidraw: edit an Excalidraw drawing in a tubeless pane.
//
//   tubeless exec excalidraw <file.excalidraw | file.excalidraw.md>
//
// Ctrl+S saves (to the same file, in the same format), Ctrl+Q quits and
// prints the file name. Obsidian's .excalidraw.md files keep their
// markdown; only the drawing block is replaced. A missing file starts
// an empty drawing and is created on the first save.
import { createRoot } from 'react-dom/client';
import { useEffect, useState } from 'react';
import { Excalidraw, serializeAsJSON } from '@excalidraw/excalidraw';
import type { ExcalidrawImperativeAPI } from '@excalidraw/excalidraw/types';
import '@excalidraw/excalidraw/index.css';
import { compressToBase64, decompressFromBase64 } from 'lz-string';
import './style.css';

declare global {
	interface Window {
		EXCALIDRAW_ASSET_PATH: string;
	}
}

// Fonts load from the applet folder, never from a CDN.
window.EXCALIDRAW_ASSET_PATH = '/node_modules/@excalidraw/excalidraw/dist/prod/';

const file = tubeless.args[0] ?? '';
const isMarkdown = file.endsWith('.md');
const blockPattern = /```(compressed-json|json)\n([\s\S]*?)\n```/;

interface Scene {
	elements: unknown[];
	appState: Record<string, unknown>;
	files: Record<string, unknown>;
}

// load reads the drawing; markdown is kept to write back around it.
async function load(): Promise<{ scene: Scene | null; markdown: string }> {
	const text = await tubeless.readText(file).catch(() => '');
	if (!text) return { scene: null, markdown: '' };
	if (!isMarkdown) return { scene: JSON.parse(text), markdown: '' };
	const block = text.match(blockPattern);
	if (!block) return { scene: null, markdown: text };
	const json = block[1] === 'compressed-json' ? decompressFromBase64(block[2].replace(/\s/g, '')) : block[2];
	return { scene: JSON.parse(json ?? 'null'), markdown: text };
}

// encode puts the drawing back into the file's own format.
function encode(json: string, markdown: string): string {
	if (!isMarkdown) return json;
	const block = '```compressed-json\n' + compressToBase64(json) + '\n```';
	if (blockPattern.test(markdown)) return markdown.replace(blockPattern, block);
	return markdownTemplate(block);
}

function markdownTemplate(block: string): string {
	return [
		'---', '', 'excalidraw-plugin: parsed', 'tags: [excalidraw]', '', '---',
		'', '# Excalidraw Data', '', '## Drawing', block, '',
	].join('\n');
}

function App({ scene, markdown }: { scene: Scene | null; markdown: string }) {
	const [theme, setTheme] = useState(tubeless.theme);
	const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null);
	useEffect(() => {
		tubeless.on('theme', () => setTheme(tubeless.theme));
	}, []);
	useEffect(() => { fileMarkdown = markdown; }, [markdown]);
	saveScene = () => save(api);
	return (
		<Excalidraw
			excalidrawAPI={setApi}
			theme={theme.isDark ? 'dark' : 'light'}
			initialData={{
				elements: (scene?.elements ?? []) as never,
				// Transparent, so the terminal bg shows through: the dark theme
				// inverts the canvas, which would turn any real color light.
				appState: { ...(scene?.appState ?? {}), viewBackgroundColor: 'transparent' },
				files: (scene?.files ?? {}) as never,
			}}
			UIOptions={{ canvasActions: { loadScene: false, saveToActiveFile: false, export: false } }}
		/>
	);
}

// fileMarkdown is the markdown around the drawing as last read or
// written, so a save keeps the rest of an .excalidraw.md file.
let fileMarkdown = '';
let saveScene: () => Promise<void> = async () => {};

async function save(api: ExcalidrawImperativeAPI | null) {
	if (!api) return;
	const json = serializeAsJSON(api.getSceneElements(), api.getAppState(), api.getFiles(), 'local');
	const text = encode(json, fileMarkdown);
	await tubeless.writeFile(file, text);
	fileMarkdown = text;
	flash(`saved ${file}`);
}

addEventListener('keydown', (e) => {
	if (!e.ctrlKey) return;
	if (e.key === 's') {
		e.preventDefault();
		e.stopPropagation();
		saveScene().catch((err) => flash(String(err)));
	}
	if (e.key === 'q') {
		e.preventDefault();
		tubeless.exit(0, file + '\n');
	}
}, true);

function flash(text: string) {
	const note = document.createElement('div');
	note.className = 'flash';
	note.textContent = text;
	document.body.append(note);
	setTimeout(() => note.remove(), 2000);
}

async function main() {
	if (!file) {
		tubeless.exit(2, 'usage: tubeless exec excalidraw <file.excalidraw | file.excalidraw.md>\n');
		return;
	}
	const { scene, markdown } = await load();
	const root = document.createElement('div');
	root.id = 'root';
	document.body.append(root);
	createRoot(root).render(<App scene={scene} markdown={markdown} />);
}

main().catch((err) => flash(String(err)));
