import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import {
	LanguageClient,
	LanguageClientOptions,
	ServerOptions,
	TransportKind,
	RevealOutputChannelOn,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;
let output: vscode.OutputChannel;

/**
 * Locates the bundled CLI for the running platform.
 *
 * Each published VSIX targets one platform, so `bin/` normally holds a single
 * binary; the per-platform subdirectory is still checked so that a locally
 * built multi-target tree keeps working.
 */
function findBundledCli(extensionPath: string): string | undefined {
	const binDir = path.join(extensionPath, 'bin');
	const goos = process.platform === 'win32' ? 'windows' : process.platform;
	const goarch = process.arch === 'x64' ? 'amd64' : process.arch;
	const binName = goos === 'windows' ? 'hclschema-cli.exe' : 'hclschema-cli';

	for (const candidate of [
		path.join(binDir, `${goos}-${goarch}`, binName),
		path.join(binDir, binName),
	]) {
		try {
			if (fs.existsSync(candidate)) {
				return candidate;
			}
		} catch {
			// Ignore and try the next candidate.
		}
	}
	return undefined;
}

function resolveCli(context: vscode.ExtensionContext): string | undefined {
	const configured = vscode.workspace.getConfiguration('hclSchema').get<string>('cliPath');
	if (configured && configured.length > 0) {
		return configured;
	}
	return findBundledCli(context.extensionPath);
}

function serverArguments(): string[] {
	const config = vscode.workspace.getConfiguration('hclSchema');
	const args = ['lsp'];
	if (config.get<boolean>('strict')) {
		args.push('--strict');
	}
	if (config.get<boolean>('offline')) {
		args.push('--offline');
	}
	const cacheDir = config.get<string>('cacheDir');
	if (cacheDir && cacheDir.length > 0) {
		args.push('--cache-dir', cacheDir);
	}
	return args;
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
	const command = resolveCli(context);
	if (!command) {
		vscode.window.showErrorMessage(
			'hcl-schema: no bundled hclschema CLI was found and `hclSchema.cliPath` is not set.',
		);
		return;
	}

	const serverOptions: ServerOptions = {
		command,
		args: serverArguments(),
		transport: TransportKind.stdio,
	};

	const clientOptions: LanguageClientOptions = {
		documentSelector: [
			{ scheme: 'file', language: 'hcl' },
			{ scheme: 'file', pattern: '**/*.hcl' },
			{ scheme: 'file', pattern: '**/*.hcl.json' },
		],
		outputChannel: output,
		// A schema parse failure is reported as a diagnostic, so there is no
		// reason to steal focus with the output panel.
		revealOutputChannelOn: RevealOutputChannelOn.Never,
		synchronize: {
			fileEvents: vscode.workspace.createFileSystemWatcher('**/*.schema.hcl'),
		},
	};

	client = new LanguageClient('hclSchema', 'HCL Schema', serverOptions, clientOptions);
	await client.start();
}

async function stopClient(): Promise<void> {
	if (!client) {
		return;
	}
	const stopping = client;
	client = undefined;
	await stopping.stop();
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
	output = vscode.window.createOutputChannel('HCL Schema');
	context.subscriptions.push(output);

	await startClient(context);

	context.subscriptions.push(
		vscode.commands.registerCommand('hcl-schema.restart', async () => {
			await stopClient();
			await startClient(context);
			vscode.window.showInformationMessage('HCL Schema: language server restarted');
		}),
	);

	context.subscriptions.push(
		vscode.commands.registerCommand('hcl-schema.showOutput', () => output.show()),
	);

	// Validation is continuous now that the server holds the buffer, so the old
	// command only needs to make sure the server is running.
	context.subscriptions.push(
		vscode.commands.registerCommand('hcl-schema.validateActive', async () => {
			if (!vscode.window.activeTextEditor) {
				vscode.window.showInformationMessage('No active editor');
				return;
			}
			if (!client) {
				await startClient(context);
			}
		}),
	);

	context.subscriptions.push(
		vscode.workspace.onDidChangeConfiguration(async (e) => {
			if (
				e.affectsConfiguration('hclSchema.cliPath') ||
				e.affectsConfiguration('hclSchema.strict') ||
				e.affectsConfiguration('hclSchema.offline') ||
				e.affectsConfiguration('hclSchema.cacheDir')
			) {
				await stopClient();
				await startClient(context);
			}
		}),
	);
}

export async function deactivate(): Promise<void> {
	await stopClient();
}
