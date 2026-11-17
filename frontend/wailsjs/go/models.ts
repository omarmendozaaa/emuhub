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
	    remoteHd: boolean;
	    remoteServer: string;
	    remoteKey: string;
	    remoteName: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minimizeOnLaunch = source["minimizeOnLaunch"];
	        this.autoCovers = source["autoCovers"];
	        this.remoteHd = source["remoteHd"];
	        this.remoteServer = source["remoteServer"];
	        this.remoteKey = source["remoteKey"];
	        this.remoteName = source["remoteName"];
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
	export class RemoteState {
	    role: string;
	    phase: string;
	    gameId: string;
	    code: string;
	    guests: number;
	    message: string;
	    warning: string;
	    players: number;
	    hostPlays: boolean;
	    seats: remote.Seat[];
	    seatControl: boolean;
	    multitap: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.phase = source["phase"];
	        this.gameId = source["gameId"];
	        this.code = source["code"];
	        this.guests = source["guests"];
	        this.message = source["message"];
	        this.warning = source["warning"];
	        this.players = source["players"];
	        this.hostPlays = source["hostPlays"];
	        this.seats = this.convertValues(source["seats"], remote.Seat);
	        this.seatControl = source["seatControl"];
	        this.multitap = source["multitap"];
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
	export class RemoteSupport {
	    host: boolean;
	    hostReason: string;
	    guest: boolean;
	    guestReason: string;
	    keyboard: boolean;
	    keyboardReason: string;
	    keyboardLayout: remote.KeyBinding[];
	
	    static createFrom(source: any = {}) {
	        return new RemoteSupport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.hostReason = source["hostReason"];
	        this.guest = source["guest"];
	        this.guestReason = source["guestReason"];
	        this.keyboard = source["keyboard"];
	        this.keyboardReason = source["keyboardReason"];
	        this.keyboardLayout = this.convertValues(source["keyboardLayout"], remote.KeyBinding);
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
	
	export class RoomLimits {
	    maxPlayers: number;
	    seatControl: boolean;
	    multitapOver: number;
	
	    static createFrom(source: any = {}) {
	        return new RoomLimits(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxPlayers = source["maxPlayers"];
	        this.seatControl = source["seatControl"];
	        this.multitapOver = source["multitapOver"];
	    }
	}

}

export namespace remote {
	
	export class KeyBinding {
	    keys: string;
	    action: string;
	
	    static createFrom(source: any = {}) {
	        return new KeyBinding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.keys = source["keys"];
	        this.action = source["action"];
	    }
	}
	export class Seat {
	    kind: string;
	    guest: number;
	    name: string;
	    ready: boolean;
	    input: string;
	
	    static createFrom(source: any = {}) {
	        return new Seat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.guest = source["guest"];
	        this.name = source["name"];
	        this.ready = source["ready"];
	        this.input = source["input"];
	    }
	}

}

