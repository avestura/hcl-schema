import * as assert from 'assert';
import * as vscode from 'vscode';

suite('HCL Schema extension', () => {
	test('activates and registers its commands', async () => {
		const ext = vscode.extensions.getExtension('avestura.hcl-schema');
		assert.ok(ext, 'extension should be discoverable');
		await ext!.activate();

		const commands = await vscode.commands.getCommands(true);
		for (const id of [
			'hcl-schema.validateActive',
			'hcl-schema.restart',
			'hcl-schema.showOutput',
		]) {
			assert.ok(commands.includes(id), `command ${id} should be registered`);
		}
	});
});
