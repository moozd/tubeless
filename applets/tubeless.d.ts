interface TubelessTheme {
	fg: string; bg: string; palette: string[]; cursor: string;
	selection: { fg: string; bg: string };
	font: { family: string; size: number };
	cell: { width: number; height: number };
	isDark: boolean;
}
interface Tubeless {
	name: string; args: string[]; cwd: string; theme: TubelessTheme;
	readonly canvas: HTMLCanvasElement;
	readFile(path: string): Promise<Uint8Array<ArrayBuffer>>;
	readText(path: string): Promise<string>;
	writeFile(path: string, data: Uint8Array | string): Promise<void>;
	exit(code?: number, output?: string): void;
	on(event: 'resize' | 'focus' | 'blur' | 'theme', fn: (theme?: TubelessTheme) => void): void;
}
declare const tubeless: Tubeless;
