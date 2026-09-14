export namespace apperr {
	
	export class Error {
	    code: string;
	    message: string;
	    details?: string[];
	    operation?: string;
	
	    static createFrom(source: any = {}) {
	        return new Error(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.details = source["details"];
	        this.operation = source["operation"];
	    }
	}

}

export namespace applications {
	
	export class Application {
	    name: string;
	    bundleId: string;
	    path: string;
	    executable: string;
	    matchKey: string;
	    matchValue: string;
	
	    static createFrom(source: any = {}) {
	        return new Application(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.bundleId = source["bundleId"];
	        this.path = source["path"];
	        this.executable = source["executable"];
	        this.matchKey = source["matchKey"];
	        this.matchValue = source["matchValue"];
	    }
	}

}

export namespace apps {
	
	export class List {
	    supported: boolean;
	    reason?: string;
	    applications: applications.Application[];
	
	    static createFrom(source: any = {}) {
	        return new List(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.reason = source["reason"];
	        this.applications = this.convertValues(source["applications"], applications.Application);
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

export namespace binary {
	
	export class UpdateInfo {
	    version: string;
	    tag: string;
	    assetName: string;
	    size: number;
	    sha256: string;
	    // Go type: time
	    publishedAt: any;
	    notes?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.tag = source["tag"];
	        this.assetName = source["assetName"];
	        this.size = source["size"];
	        this.sha256 = source["sha256"];
	        this.publishedAt = this.convertValues(source["publishedAt"], null);
	        this.notes = source["notes"];
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
	export class CheckResult {
	    currentVersion: string;
	    // Go type: time
	    checkedAt: any;
	    update?: UpdateInfo;
	    unsupported: boolean;
	    noInstalledBinary: boolean;
	    downloadHost?: string;
	    updateAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.checkedAt = this.convertValues(source["checkedAt"], null);
	        this.update = this.convertValues(source["update"], UpdateInfo);
	        this.unsupported = source["unsupported"];
	        this.noInstalledBinary = source["noInstalledBinary"];
	        this.downloadHost = source["downloadHost"];
	        this.updateAvailable = source["updateAvailable"];
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
	export class ManagedInfo {
	    installed: boolean;
	    version?: string;
	    path?: string;
	    sha256?: string;
	    // Go type: time
	    installedAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new ManagedInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.sha256 = source["sha256"];
	        this.installedAt = this.convertValues(source["installedAt"], null);
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
	export class Status {
	    source: string;
	    platform: string;
	    unsupported: boolean;
	    activePath: string;
	    activeVersion: string;
	    activeOk: boolean;
	    activeError?: string;
	    managed: ManagedInfo;
	    customPath?: string;
	    // Go type: time
	    lastCheck?: any;
	    checkError?: string;
	    update?: UpdateInfo;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.platform = source["platform"];
	        this.unsupported = source["unsupported"];
	        this.activePath = source["activePath"];
	        this.activeVersion = source["activeVersion"];
	        this.activeOk = source["activeOk"];
	        this.activeError = source["activeError"];
	        this.managed = this.convertValues(source["managed"], ManagedInfo);
	        this.customPath = source["customPath"];
	        this.lastCheck = this.convertValues(source["lastCheck"], null);
	        this.checkError = source["checkError"];
	        this.update = this.convertValues(source["update"], UpdateInfo);
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
	export class InstallResult {
	    version: string;
	    path: string;
	    sha256: string;
	    previousPath?: string;
	    staleRevisions: number;
	    // Go type: time
	    installedAt: any;
	    status: Status;
	
	    static createFrom(source: any = {}) {
	        return new InstallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.path = source["path"];
	        this.sha256 = source["sha256"];
	        this.previousPath = source["previousPath"];
	        this.staleRevisions = source["staleRevisions"];
	        this.installedAt = this.convertValues(source["installedAt"], null);
	        this.status = this.convertValues(source["status"], Status);
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

export namespace config {
	
	export class ApplyInput {
	    profileId: string;
	    revisionId: string;
	
	    static createFrom(source: any = {}) {
	        return new ApplyInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.revisionId = source["revisionId"];
	    }
	}
	export class ApplyResult {
	    revisionId: string;
	    profileId: string;
	    activeConfigPath: string;
	    restarted: boolean;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revisionId = source["revisionId"];
	        this.profileId = source["profileId"];
	        this.activeConfigPath = source["activeConfigPath"];
	        this.restarted = source["restarted"];
	        this.warning = source["warning"];
	    }
	}
	export class DiffLine {
	    kind: string;
	    left: number;
	    right: number;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.left = source["left"];
	        this.right = source["right"];
	        this.text = source["text"];
	    }
	}
	export class Diff {
	    leftLabel: string;
	    rightLabel: string;
	    lines: DiffLine[];
	    added: number;
	    removed: number;
	    identical: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Diff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.leftLabel = source["leftLabel"];
	        this.rightLabel = source["rightLabel"];
	        this.lines = this.convertValues(source["lines"], DiffLine);
	        this.added = source["added"];
	        this.removed = source["removed"];
	        this.identical = source["identical"];
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
	
	export class Result {
	    ok: boolean;
	    errors: string[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.errors = source["errors"];
	        this.warnings = source["warnings"];
	    }
	}
	export class Draft {
	    profileId: string;
	    profileName: string;
	    baseRevisionId: string;
	    baseSource: string;
	    configJson: string;
	    singBoxVersion: string;
	    validation: string;
	    structural: Result;
	
	    static createFrom(source: any = {}) {
	        return new Draft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.profileName = source["profileName"];
	        this.baseRevisionId = source["baseRevisionId"];
	        this.baseSource = source["baseSource"];
	        this.configJson = source["configJson"];
	        this.singBoxVersion = source["singBoxVersion"];
	        this.validation = source["validation"];
	        this.structural = this.convertValues(source["structural"], Result);
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
	export class LegacyCandidate {
	    found: boolean;
	    configPath: string;
	    settingsPath?: string;
	    binaryPath?: string;
	    autoConnect?: boolean;
	    valid: boolean;
	    size: number;
	    // Go type: time
	    modified: any;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new LegacyCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.found = source["found"];
	        this.configPath = source["configPath"];
	        this.settingsPath = source["settingsPath"];
	        this.binaryPath = source["binaryPath"];
	        this.autoConnect = source["autoConnect"];
	        this.valid = source["valid"];
	        this.size = source["size"];
	        this.modified = this.convertValues(source["modified"], null);
	        this.warnings = source["warnings"];
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
	
	export class RevisionView {
	    id: string;
	    profileId: string;
	    parentRevisionId?: string;
	    // Go type: time
	    createdAt: any;
	    source: string;
	    configJson: string;
	    structuralValidationStatus: string;
	    singBoxValidationStatus: string;
	    singBoxVersion: string;
	    comment?: string;
	    active: boolean;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new RevisionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.profileId = source["profileId"];
	        this.parentRevisionId = source["parentRevisionId"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.source = source["source"];
	        this.configJson = source["configJson"];
	        this.structuralValidationStatus = source["structuralValidationStatus"];
	        this.singBoxValidationStatus = source["singBoxValidationStatus"];
	        this.singBoxVersion = source["singBoxVersion"];
	        this.comment = source["comment"];
	        this.active = source["active"];
	        this.size = source["size"];
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
	export class SaveInput {
	    profileId: string;
	    configJson: string;
	    comment: string;
	    source: string;
	    apply: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.configJson = source["configJson"];
	        this.comment = source["comment"];
	        this.source = source["source"];
	        this.apply = source["apply"];
	    }
	}
	export class ValidateInput {
	    profileId: string;
	    configJson: string;
	    skipSingBoxCheck: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ValidateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.configJson = source["configJson"];
	        this.skipSingBoxCheck = source["skipSingBoxCheck"];
	    }
	}
	export class ValidateResult {
	    structural: Result;
	    check?: singbox.CheckResult;
	    valid: boolean;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new ValidateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.structural = this.convertValues(source["structural"], Result);
	        this.check = this.convertValues(source["check"], singbox.CheckResult);
	        this.valid = source["valid"];
	        this.version = source["version"];
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

export namespace desktop {
	
	export class ActiveConfigPayload {
	    path: string;
	    profileId: string;
	    configJson: string;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ActiveConfigPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.profileId = source["profileId"];
	        this.configJson = source["configJson"];
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ApplyPayload {
	    result: config.ApplyResult;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ApplyPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.result = this.convertValues(source["result"], config.ApplyResult);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class AppsPayload {
	    apps: apps.List;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new AppsPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apps = this.convertValues(source["apps"], apps.List);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class BinaryCheckPayload {
	    check: binary.CheckResult;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new BinaryCheckPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.check = this.convertValues(source["check"], binary.CheckResult);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class BinaryInstallPayload {
	    install: binary.InstallResult;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new BinaryInstallPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.install = this.convertValues(source["install"], binary.InstallResult);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class BinaryStatusPayload {
	    status: binary.Status;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new BinaryStatusPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = this.convertValues(source["status"], binary.Status);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class BinaryVersionPayload {
	    version: singbox.Version;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new BinaryVersionPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = this.convertValues(source["version"], singbox.Version);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class CreateProfileRequest {
	    name: string;
	    description: string;
	    templateId: string;
	    configJson: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateProfileRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.templateId = source["templateId"];
	        this.configJson = source["configJson"];
	    }
	}
	export class DiffPayload {
	    diff: config.Diff;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new DiffPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.diff = this.convertValues(source["diff"], config.Diff);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class DraftPayload {
	    draft: config.Draft;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new DraftPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.draft = this.convertValues(source["draft"], config.Draft);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class Environment {
	    version: string;
	    os: string;
	    arch: string;
	    supported: boolean;
	    unsupportedReason?: string;
	    execPath: string;
	    dataDir: string;
	    configDir: string;
	    activeConfigPath: string;
	    lastGoodConfigPath: string;
	    logPath: string;
	    binDir: string;
	    runtimeDir: string;
	    shareSchemes: string[];
	    migration?: config.LegacyCandidate;
	
	    static createFrom(source: any = {}) {
	        return new Environment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.supported = source["supported"];
	        this.unsupportedReason = source["unsupportedReason"];
	        this.execPath = source["execPath"];
	        this.dataDir = source["dataDir"];
	        this.configDir = source["configDir"];
	        this.activeConfigPath = source["activeConfigPath"];
	        this.lastGoodConfigPath = source["lastGoodConfigPath"];
	        this.logPath = source["logPath"];
	        this.binDir = source["binDir"];
	        this.runtimeDir = source["runtimeDir"];
	        this.shareSchemes = source["shareSchemes"];
	        this.migration = this.convertValues(source["migration"], config.LegacyCandidate);
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
	export class EnvironmentPayload {
	    environment: Environment;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new EnvironmentPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.environment = this.convertValues(source["environment"], Environment);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ExportPayload {
	    export?: profiles.ExportResult;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ExportPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.export = this.convertValues(source["export"], profiles.ExportResult);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ImportConfigFileRequest {
	    path: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportConfigFileRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	    }
	}
	export class LegacyPayload {
	    candidate: config.LegacyCandidate;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new LegacyPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.candidate = this.convertValues(source["candidate"], config.LegacyCandidate);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class LogsPayload {
	    records: runtime.LogRecord[];
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new LogsPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.records = this.convertValues(source["records"], runtime.LogRecord);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class PickConfigFilePayload {
	    path: string;
	    canceled: boolean;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new PickConfigFilePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.canceled = source["canceled"];
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ProfilePayload {
	    profile: profile.Profile;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ProfilePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profile = this.convertValues(source["profile"], profile.Profile);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ProfilesPayload {
	    profiles: profile.Profile[];
	    activeId: string;
	    running: boolean;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ProfilesPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profiles = this.convertValues(source["profiles"], profile.Profile);
	        this.activeId = source["activeId"];
	        this.running = source["running"];
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class RevisionListPayload {
	    revisions: config.RevisionView[];
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new RevisionListPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revisions = this.convertValues(source["revisions"], config.RevisionView);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class RevisionPayload {
	    revision: config.RevisionView;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new RevisionPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = this.convertValues(source["revision"], config.RevisionView);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class RevisionsPayload {
	    profileId: string;
	    revisions: config.RevisionView[];
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new RevisionsPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.revisions = this.convertValues(source["revisions"], config.RevisionView);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class RuntimePayload {
	    status: runtime.Status;
	    activeProfileName: string;
	    binaryVersion: string;
	    trafficAvailable: boolean;
	    shuttingDown: boolean;
	    foreignProcesses: runtime.ForeignProcess[];
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new RuntimePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = this.convertValues(source["status"], runtime.Status);
	        this.activeProfileName = source["activeProfileName"];
	        this.binaryVersion = source["binaryVersion"];
	        this.trafficAvailable = source["trafficAvailable"];
	        this.shuttingDown = source["shuttingDown"];
	        this.foreignProcesses = this.convertValues(source["foreignProcesses"], runtime.ForeignProcess);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class SettingsPayload {
	    state: settings.State;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new SettingsPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = this.convertValues(source["state"], settings.State);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ShareBuildPayload {
	    link: string;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ShareBuildPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.link = source["link"];
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ShareListPayload {
	    parsed: share.Parsed[];
	    errors?: string[];
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ShareListPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.parsed = this.convertValues(source["parsed"], share.Parsed);
	        this.errors = source["errors"];
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ShareParsePayload {
	    parsed?: share.Parsed;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ShareParsePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.parsed = this.convertValues(source["parsed"], share.Parsed);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class TemplatesPayload {
	    templates: templates.Template[];
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new TemplatesPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.templates = this.convertValues(source["templates"], templates.Template);
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class TrafficPayload {
	    snapshot: traffic.Snapshot;
	    collectorRunning: boolean;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new TrafficPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.snapshot = this.convertValues(source["snapshot"], traffic.Snapshot);
	        this.collectorRunning = source["collectorRunning"];
	        this.error = this.convertValues(source["error"], apperr.Error);
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
	export class ValidatePayload {
	    result: config.ValidateResult;
	    error?: apperr.Error;
	
	    static createFrom(source: any = {}) {
	        return new ValidatePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.result = this.convertValues(source["result"], config.ValidateResult);
	        this.error = this.convertValues(source["error"], apperr.Error);
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

export namespace profile {
	
	export class Profile {
	    id: string;
	    name: string;
	    description: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    activeRevisionId: string;
	    // Go type: time
	    lastUsedAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.activeRevisionId = source["activeRevisionId"];
	        this.lastUsedAt = this.convertValues(source["lastUsedAt"], null);
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

export namespace profiles {
	
	export class ExportResult {
	    fileName: string;
	    configJson: string;
	    revisionId: string;
	    singBoxVersion?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.configJson = source["configJson"];
	        this.revisionId = source["revisionId"];
	        this.singBoxVersion = source["singBoxVersion"];
	    }
	}

}

export namespace runtime {
	
	export class ForeignProcess {
	    pid: number;
	    revisionId: string;
	    pidPath: string;
	    binaryPath: string;
	    // Go type: time
	    startedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new ForeignProcess(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pid = source["pid"];
	        this.revisionId = source["revisionId"];
	        this.pidPath = source["pidPath"];
	        this.binaryPath = source["binaryPath"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
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
	export class LogQuery {
	    limit: number;
	    afterSeq: number;
	    filter: string;
	    level: string;
	
	    static createFrom(source: any = {}) {
	        return new LogQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.limit = source["limit"];
	        this.afterSeq = source["afterSeq"];
	        this.filter = source["filter"];
	        this.level = source["level"];
	    }
	}
	export class LogRecord {
	    seq: number;
	    // Go type: time
	    time: any;
	    source: string;
	    level: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LogRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.time = this.convertValues(source["time"], null);
	        this.source = source["source"];
	        this.level = source["level"];
	        this.message = source["message"];
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
	export class Status {
	    state: string;
	    pid: number;
	    // Go type: time
	    startedAt?: any;
	    uptimeSeconds: number;
	    activeProfileId: string;
	    activeRevisionId: string;
	    binaryVersion: string;
	    configPath: string;
	    elevated: boolean;
	    lastExitCode?: number;
	    lastError?: string;
	    lastErrorCode?: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.pid = source["pid"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.uptimeSeconds = source["uptimeSeconds"];
	        this.activeProfileId = source["activeProfileId"];
	        this.activeRevisionId = source["activeRevisionId"];
	        this.binaryVersion = source["binaryVersion"];
	        this.configPath = source["configPath"];
	        this.elevated = source["elevated"];
	        this.lastExitCode = source["lastExitCode"];
	        this.lastError = source["lastError"];
	        this.lastErrorCode = source["lastErrorCode"];
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

export namespace settings {
	
	export class AutostartState {
	    supported: boolean;
	    enabled: boolean;
	    legacyEntry?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AutostartState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.enabled = source["enabled"];
	        this.legacyEntry = source["legacyEntry"];
	        this.error = source["error"];
	    }
	}
	export class Settings {
	    autoStartApplication: boolean;
	    autoConnect: boolean;
	    binarySource: string;
	    customBinaryPath: string;
	    managedStableChannel: boolean;
	    updateCheckEnabled: boolean;
	    theme: string;
	    logLevel: string;
	    lastProfileId: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoStartApplication = source["autoStartApplication"];
	        this.autoConnect = source["autoConnect"];
	        this.binarySource = source["binarySource"];
	        this.customBinaryPath = source["customBinaryPath"];
	        this.managedStableChannel = source["managedStableChannel"];
	        this.updateCheckEnabled = source["updateCheckEnabled"];
	        this.theme = source["theme"];
	        this.logLevel = source["logLevel"];
	        this.lastProfileId = source["lastProfileId"];
	    }
	}
	export class State {
	    values: Settings;
	    autostart: AutostartState;
	    dataDir: string;
	    logPath: string;
	    binaryPath: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.values = this.convertValues(source["values"], Settings);
	        this.autostart = this.convertValues(source["autostart"], AutostartState);
	        this.dataDir = source["dataDir"];
	        this.logPath = source["logPath"];
	        this.binaryPath = source["binaryPath"];
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
	export class UpdateInput {
	    autoStartApplication?: boolean;
	    autoConnect?: boolean;
	    binarySource?: string;
	    customBinaryPath?: string;
	    managedStableChannel?: boolean;
	    updateCheckEnabled?: boolean;
	    theme?: string;
	    logLevel?: string;
	    lastProfileId?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoStartApplication = source["autoStartApplication"];
	        this.autoConnect = source["autoConnect"];
	        this.binarySource = source["binarySource"];
	        this.customBinaryPath = source["customBinaryPath"];
	        this.managedStableChannel = source["managedStableChannel"];
	        this.updateCheckEnabled = source["updateCheckEnabled"];
	        this.theme = source["theme"];
	        this.logLevel = source["logLevel"];
	        this.lastProfileId = source["lastProfileId"];
	    }
	}

}

export namespace share {
	
	export class Parsed {
	    kind: string;
	    tag: string;
	    outbound: Record<string, any>;
	    displayName: string;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Parsed(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.tag = source["tag"];
	        this.outbound = source["outbound"];
	        this.displayName = source["displayName"];
	        this.warnings = source["warnings"];
	    }
	}

}

export namespace singbox {
	
	export class CheckResult {
	    ok: boolean;
	    version: string;
	    output: string;
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.version = source["version"];
	        this.output = source["output"];
	        this.errors = source["errors"];
	    }
	}
	export class Version {
	    Major: number;
	    Minor: number;
	    Patch: number;
	    Pre: string;
	    Raw: string;
	
	    static createFrom(source: any = {}) {
	        return new Version(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Major = source["Major"];
	        this.Minor = source["Minor"];
	        this.Patch = source["Patch"];
	        this.Pre = source["Pre"];
	        this.Raw = source["Raw"];
	    }
	}

}

export namespace templates {
	
	export class Template {
	    id: string;
	    name: string;
	    description: string;
	    config: string;
	    requiresPrivilege: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Template(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.config = source["config"];
	        this.requiresPrivilege = source["requiresPrivilege"];
	    }
	}

}

export namespace traffic {
	
	export class Connection {
	    id: string;
	    host?: string;
	    network?: string;
	    rule?: string;
	    outbound?: string;
	    upload: number;
	    download: number;
	    started?: string;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.host = source["host"];
	        this.network = source["network"];
	        this.rule = source["rule"];
	        this.outbound = source["outbound"];
	        this.upload = source["upload"];
	        this.download = source["download"];
	        this.started = source["started"];
	    }
	}
	export class OutboundTraffic {
	    outbound: string;
	    upload: number;
	    download: number;
	    connections: number;
	
	    static createFrom(source: any = {}) {
	        return new OutboundTraffic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outbound = source["outbound"];
	        this.upload = source["upload"];
	        this.download = source["download"];
	        this.connections = source["connections"];
	    }
	}
	export class Snapshot {
	    // Go type: time
	    timestamp: any;
	    profileId?: string;
	    revisionId?: string;
	    connections: number;
	    uploadTotal: number;
	    downloadTotal: number;
	    uploadRate: number;
	    downloadRate: number;
	    byOutbound: OutboundTraffic[];
	    active: Connection[];
	    available: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.profileId = source["profileId"];
	        this.revisionId = source["revisionId"];
	        this.connections = source["connections"];
	        this.uploadTotal = source["uploadTotal"];
	        this.downloadTotal = source["downloadTotal"];
	        this.uploadRate = source["uploadRate"];
	        this.downloadRate = source["downloadRate"];
	        this.byOutbound = this.convertValues(source["byOutbound"], OutboundTraffic);
	        this.active = this.convertValues(source["active"], Connection);
	        this.available = source["available"];
	        this.error = source["error"];
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

