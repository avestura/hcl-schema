import * as vscode from 'vscode';
import { execFile } from 'child_process';
import * as path from 'path';
import * as fs from 'fs';

interface OutDiagnostic {
	file: string;
	startLine: number;
	startCol: number;
	endLine: number;
	endCol: number;
	severity: 'error' | 'warning' | 'info';
	message: string;
}

let diagnosticCollection: vscode.DiagnosticCollection;
const debounceTimers = new Map<string, NodeJS.Timeout>();
let bundledCliPath: string | null = null;

function toVSCodeSeverity(s: string): vscode.DiagnosticSeverity {
	switch (s) {
		case 'error':
			return vscode.DiagnosticSeverity.Error;
		case 'warning':
			return vscode.DiagnosticSeverity.Warning;
		default:
			return vscode.DiagnosticSeverity.Information;
	}
}

function runValidatorOnFile(hclPath: string): Promise<OutDiagnostic[]> {
	return new Promise((resolve, reject) => {
			const config = vscode.workspace.getConfiguration('hclSchema');
			const cliPath = config.get<string>('cliPath') || '';

			let cmd: string;
			let args: string[];
			if (cliPath && cliPath.length > 0) {
				cmd = cliPath;
				args = ['--detect', hclPath];
			} else if (bundledCliPath) {
				cmd = bundledCliPath;
				args = ['--detect', hclPath];
			} else {
				// Do not attempt to run `go run` from the extension for security reasons.
				const msg = 'hcl-schema: no bundled CLI found and `hclSchema.cliPath` is not configured. Install the bundled binary or set `hclSchema.cliPath` in settings.';
				vscode.window.showErrorMessage(msg);
				return reject(new Error(msg));
			}

			execFile(cmd, args, { cwd: path.resolve(__dirname, '..', '..') }, (err, stdout, stderr) => {
			if (err) {
				return reject(new Error(stderr || err.message));
			}
			try {
				const parsed = JSON.parse(stdout) as OutDiagnostic[];
				resolve(parsed);
			} catch (e) {
				return reject(new Error('failed to parse validator output: ' + e));
			}
		});
	});
}

function scheduleValidate(document: vscode.TextDocument) {
	if (document.languageId !== 'hcl' && !document.fileName.endsWith('.hcl')) {
		return;
	}
	const key = document.uri.toString();
	if (debounceTimers.has(key)) {
		clearTimeout(debounceTimers.get(key)!);
	}
	const timer = setTimeout(async () => {
		debounceTimers.delete(key);
		try {
			const out = await runValidatorOnFile(document.fileName);
			const diagnostics: vscode.Diagnostic[] = [];
			for (const d of out) {
				const range = new vscode.Range(d.startLine, d.startCol, d.endLine, d.endCol);
				const diag = new vscode.Diagnostic(range, d.message, toVSCodeSeverity(d.severity));
				diagnostics.push(diag);
			}
			diagnosticCollection.set(document.uri, diagnostics);
		} catch (e: any) {
			console.error('hcl-schema validator error:', e);
			const diag = new vscode.Diagnostic(new vscode.Range(0, 0, 0, 1), String(e.message || e), vscode.DiagnosticSeverity.Error);
			diagnosticCollection.set(document.uri, [diag]);
		}
	}, 250);
	debounceTimers.set(key, timer);
}

export function activate(context: vscode.ExtensionContext) {
	console.log('hcl-schema extension active');

	diagnosticCollection = vscode.languages.createDiagnosticCollection('hcl-schema');
	context.subscriptions.push(diagnosticCollection);

	try {
		const binDir = path.join(context.extensionPath, 'bin');
		const goos = process.platform === 'win32' ? 'windows' : process.platform;
		const goarch = process.arch === 'x64' ? 'amd64' : process.arch;
		const binName = goos === 'windows' ? 'hclschema-cli.exe' : 'hclschema-cli';
		const platformCandidate = path.join(binDir, `${goos}-${goarch}`, binName);
		const topCandidate = path.join(binDir, binName);
		if (fs.existsSync(platformCandidate)) {
			bundledCliPath = platformCandidate;
		} else if (fs.existsSync(topCandidate)) {
			bundledCliPath = topCandidate;
		} else {
			bundledCliPath = null;
		}
	} catch (e) {
		bundledCliPath = null;
	}

	for (const doc of vscode.workspace.textDocuments) {
		scheduleValidate(doc);
	}

	context.subscriptions.push(vscode.workspace.onDidOpenTextDocument((doc) => scheduleValidate(doc)));
	context.subscriptions.push(vscode.workspace.onDidSaveTextDocument((doc) => scheduleValidate(doc)));
	context.subscriptions.push(vscode.workspace.onDidChangeTextDocument((e) => scheduleValidate(e.document)));

	context.subscriptions.push(vscode.commands.registerCommand('hcl-schema.validateActive', async () => {
		const editor = vscode.window.activeTextEditor;
		if (!editor) {
			vscode.window.showInformationMessage('No active editor');
			return;
		}
		scheduleValidate(editor.document);
		vscode.window.showInformationMessage('Validation scheduled');
	}));
}

export function deactivate() {
	diagnosticCollection && diagnosticCollection.clear();
}
