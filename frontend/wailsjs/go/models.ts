export namespace library {
	
	export class DetectedEmulator {
	    label: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new DetectedEmulator(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.command = source["command"];
	    }
	}
	export class Game {
	    id: string;
	    console: string;
	    title: string;
	    fileName: string;
	    path: string;
	    serial?: string;
	    hasCover: boolean;
	    hasBackdrop: boolean;
	    coverRev: number;
	    favorite: boolean;
	    addedAt: number;
	    lastPlayed: number;
	    playSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new Game(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.console = source["console"];
	        this.title = source["title"];
	        this.fileName = source["fileName"];
	        this.path = source["path"];
	        this.serial = source["serial"];
	        this.hasCover = source["hasCover"];
	        this.hasBackdrop = source["hasBackdrop"];
	        this.coverRev = source["coverRev"];
	        this.favorite = source["favorite"];
	        this.addedAt = source["addedAt"];
	        this.lastPlayed = source["lastPlayed"];
	        this.playSeconds = source["playSeconds"];
	    }
	}
	export class Settings {
	    minimizeOnLaunch: boolean;
	    autoCovers: boolean;
	    paths: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minimizeOnLaunch = source["minimizeOnLaunch"];
	        this.autoCovers = source["autoCovers"];
	        this.paths = source["paths"];
	    }
	}

}

export namespace main {
	
	export class BundledEmuInfo {
	    name: string;
	    version: string;
	    license: string;
	    source: string;
	    consoles: string[];
	
	    static createFrom(source: any = {}) {
	        return new BundledEmuInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.license = source["license"];
	        this.source = source["source"];
	        this.consoles = source["consoles"];
	    }
	}
	export class BundleState {
	    available: boolean;
	    installing: boolean;
	    ready: boolean;
	    done: number;
	    total: number;
	    error: string;
	    version: string;
	    emulators: BundledEmuInfo[];
	
	    static createFrom(source: any = {}) {
	        return new BundleState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.installing = source["installing"];
	        this.ready = source["ready"];
	        this.done = source["done"];
	        this.total = source["total"];
	        this.error = source["error"];
	        this.version = source["version"];
	        this.emulators = this.convertValues(source["emulators"], BundledEmuInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RequirementStatus {
	    id: string;
	    label: string;
	    kind: string;
	    hint: string;
	    path: string;
	    ok: boolean;
	    detail: string;
	    files: string[];
	
	    static createFrom(source: any = {}) {
	        return new RequirementStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.hint = source["hint"];
	        this.path = source["path"];
	        this.ok = source["ok"];
	        this.detail = source["detail"];
	        this.files = source["files"];
	    }
	}
	export class ConsoleInfo {
	    id: string;
	    name: string;
	    shortName: string;
	    maker: string;
	    accent: string;
	    extensions: string[];
	    emulator: string;
	    command: string;
	    romDirs: string[];
	    bundled?: BundledEmuInfo;
	    emulatorOk: boolean;
	    requirements: RequirementStatus[];
	    ready: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConsoleInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.shortName = source["shortName"];
	        this.maker = source["maker"];
	        this.accent = source["accent"];
	        this.extensions = source["extensions"];
	        this.emulator = source["emulator"];
	        this.command = source["command"];
	        this.romDirs = source["romDirs"];
	        this.bundled = this.convertValues(source["bundled"], BundledEmuInfo);
	        this.emulatorOk = source["emulatorOk"];
	        this.requirements = this.convertValues(source["requirements"], RequirementStatus);
	        this.ready = source["ready"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

