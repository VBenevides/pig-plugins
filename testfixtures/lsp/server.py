#!/usr/bin/env python3
"""A tiny fake language server (stdio, Content-Length framing) for protocol-level tests of harness-code.

It "analyses" Python-like text with trivial rules so results are predictable:
  - diagnostics: `ERROR_TOKEN` on a line is an error, `WARN_TOKEN` a warning (published after didOpen/didChange,
    or answered to textDocument/diagnostic when FAKE_LSP_PULL=1);
  - hover: the word under the cursor; definition: first line containing `def <word>` or `class <word>`;
  - references: every whole-word occurrence in the open documents; documentSymbol: `def`/`class` lines;
  - workspace/symbol: `def`/`class` names containing the query in *.py files under the root;
  - rename: edits every whole-word occurrence in the open document. Before answering it sends the client a
    `workspace/applyEdit` request (to prove a client refuses it) and logs the client's answer.
Environment: FAKE_LSP_LOG (JSON lines of everything received and the applyEdit answer), FAKE_LSP_PID (pid file),
FAKE_LSP_MODE: normal | hang (never answers hover) | crash (exits on hover) | trickle (writes 1 byte at a time) |
slowstart (sleeps before the initialize result) | noshutdown (ignores shutdown/exit; must be killed) |
silent (never publishes diagnostics) | badframe (sends garbage after initialize).
"""
import json
import os
import re
import sys
import threading
import time
from pathlib import Path
from urllib.parse import unquote, urlparse

MODE = os.environ.get('FAKE_LSP_MODE', 'normal')
LOG = os.environ.get('FAKE_LSP_LOG')
PULL = os.environ.get('FAKE_LSP_PULL') == '1'
docs = {}
root = ['']
out_lock = threading.Lock()
pending = {}


def log(entry):
    if LOG:
        with open(LOG, 'a') as handle:
            handle.write(json.dumps(entry) + '\n')


def send(message):
    data = json.dumps(message).encode()
    frame = b'Content-Length: %d\r\n\r\n' % len(data) + data
    with out_lock:
        if MODE == 'trickle':
            for index in range(len(frame)):
                sys.stdout.buffer.write(frame[index:index + 1])
                sys.stdout.buffer.flush()
        else:
            sys.stdout.buffer.write(frame)
            sys.stdout.buffer.flush()


def read_message():
    header = b''
    while not header.endswith(b'\r\n\r\n'):
        byte = sys.stdin.buffer.read(1)
        if not byte:
            return None
        header += byte
    length = int(re.search(rb'Content-Length: (\d+)', header, re.I).group(1))
    return json.loads(sys.stdin.buffer.read(length))


def path_of(uri):
    return unquote(urlparse(uri).path)


def word_at(text, line, character):
    lines = text.split('\n')
    if line >= len(lines):
        return None, None
    for match in re.finditer(r'\w+', lines[line]):
        if match.start() <= character <= match.end():
            return match.group(0), match.start()
    return None, None


def diagnostics_of(text):
    items = []
    for number, line in enumerate(text.split('\n')):
        for token, severity in (('ERROR_TOKEN', 1), ('WARN_TOKEN', 2)):
            column = line.find(token)
            if column >= 0:
                items.append({'range': {'start': {'line': number, 'character': column},
                                        'end': {'line': number, 'character': column + len(token)}},
                              'severity': severity, 'source': 'fake', 'code': 'F001' if severity == 1 else 'F002',
                              'message': f'{token.lower()} found'})
    return items


def publish(uri):
    if MODE == 'silent':
        return
    send({'jsonrpc': '2.0', 'method': 'textDocument/publishDiagnostics',
          'params': {'uri': uri, 'diagnostics': diagnostics_of(docs[uri])}})


def symbols_of(text):
    result = []
    lines = text.split('\n')
    for number, line in enumerate(lines):
        match = re.match(r'(def|class)\s+(\w+)', line)
        if match:
            result.append({'name': match.group(2), 'kind': 12 if match.group(1) == 'def' else 5,
                           'range': {'start': {'line': number, 'character': 0}, 'end': {'line': number, 'character': len(line)}},
                           'selectionRange': {'start': {'line': number, 'character': 0}, 'end': {'line': number, 'character': len(line)}}})
    return result


def occurrences(text, word):
    found = []
    for number, line in enumerate(text.split('\n')):
        for match in re.finditer(r'\b' + re.escape(word) + r'\b', line):
            found.append({'start': {'line': number, 'character': match.start()}, 'end': {'line': number, 'character': match.end()}})
    return found


def handle_request(message):
    method, params, ident = message['method'], message.get('params') or {}, message['id']
    document = params.get('textDocument', {}).get('uri')
    text = docs.get(document, '')
    if method == 'initialize':
        root[0] = path_of(params['rootUri'])
        if MODE == 'slowstart':
            time.sleep(1.5)
        capabilities = {'textDocumentSync': 1, 'hoverProvider': True, 'definitionProvider': True, 'referencesProvider': True,
                        'documentSymbolProvider': True, 'workspaceSymbolProvider': True,
                        'renameProvider': {'prepareProvider': True}}
        if PULL:
            capabilities['diagnosticProvider'] = {'interFileDependencies': False, 'workspaceDiagnostics': False}
        send({'jsonrpc': '2.0', 'id': ident, 'result': {'capabilities': capabilities, 'serverInfo': {'name': 'fake-lsp'}}})
        return
    if method == 'shutdown':
        if MODE == 'noshutdown':
            return
        send({'jsonrpc': '2.0', 'id': ident, 'result': None})
        return
    if method == 'textDocument/hover':
        if MODE == 'hang':
            return
        if MODE == 'crash':
            os._exit(3)
        word, _ = word_at(text, params['position']['line'], params['position']['character'])
        result = None if word is None else {'contents': {'kind': 'markdown', 'value': f'**{word}** (fake hover)'}}
    elif method == 'textDocument/definition':
        word, _ = word_at(text, params['position']['line'], params['position']['character'])
        result = None
        for number, line in enumerate(text.split('\n')):
            if word and re.match(r'(def|class)\s+' + re.escape(word) + r'\b', line):
                result = [{'uri': document, 'range': {'start': {'line': number, 'character': 0}, 'end': {'line': number, 'character': len(line)}}}]
                break
    elif method == 'textDocument/references':
        word, _ = word_at(text, params['position']['line'], params['position']['character'])
        result = []
        for uri, body in docs.items():
            result += [{'uri': uri, 'range': r} for r in occurrences(body, word or '')]
    elif method == 'textDocument/documentSymbol':
        result = symbols_of(text)
    elif method == 'workspace/symbol':
        result = []
        query = params.get('query', '')
        for file in sorted(Path(root[0]).rglob('*.py')):
            for symbol in symbols_of(file.read_text()):
                if query.lower() in symbol['name'].lower():
                    result.append({'name': symbol['name'], 'kind': symbol['kind'],
                                   'location': {'uri': file.as_uri(), 'range': symbol['range']}})
    elif method == 'textDocument/prepareRename':
        word, _ = word_at(text, params['position']['line'], params['position']['character'])
        result = None if not word else {'range': occurrences(text, word)[0], 'placeholder': word}
    elif method == 'textDocument/rename':
        word, _ = word_at(text, params['position']['line'], params['position']['character'])
        request_id = 'applyEdit-1'
        send({'jsonrpc': '2.0', 'id': request_id, 'method': 'workspace/applyEdit',
              'params': {'label': 'rename', 'edit': {'changes': {document: []}}}})
        deadline = time.time() + 2
        while request_id not in pending and time.time() < deadline:
            time.sleep(0.01)
        log({'applyEditAnswer': pending.get(request_id)})
        result = {'changes': {document: [{'range': r, 'newText': params['newName']} for r in occurrences(text, word or '')]}}
        if os.environ.get('FAKE_LSP_FILEOP') == '1':
            result = {'documentChanges': [
                {'textDocument': {'uri': document, 'version': 1}, 'edits': result['changes'][document]},
                {'kind': 'rename', 'oldUri': document, 'newUri': document + '.renamed'}]}
    elif method == 'textDocument/diagnostic':
        result = {'kind': 'full', 'items': diagnostics_of(text)}
    else:
        send({'jsonrpc': '2.0', 'id': ident, 'error': {'code': -32601, 'message': f'unsupported {method}'}})
        return
    send({'jsonrpc': '2.0', 'id': ident, 'result': result})


def main():
    pid_file = os.environ.get('FAKE_LSP_PID')
    if pid_file:
        Path(pid_file).write_text(str(os.getpid()))
    while True:
        message = read_message()
        if message is None:
            return
        log({'method': message.get('method'), 'params': message.get('params') if message.get('method', '').startswith('textDocument/did') else None})
        if 'method' not in message:  # response to one of our requests
            pending[message['id']] = message.get('result', message.get('error'))
            continue
        method = message['method']
        if 'id' in message:
            # Rename waits for the client's applyEdit answer, so requests run on their own thread.
            threading.Thread(target=handle_request, args=(message,), daemon=True).start()
            continue
        params = message.get('params') or {}
        if method == 'initialized':
            send({'jsonrpc': '2.0', 'id': 'cfg-1', 'method': 'workspace/configuration', 'params': {'items': [{'section': 'a'}, {'section': 'b'}]}})
            send({'jsonrpc': '2.0', 'id': 'reg-1', 'method': 'client/registerCapability', 'params': {'registrations': []}})
            send({'jsonrpc': '2.0', 'id': 'prog-1', 'method': 'window/workDoneProgress/create', 'params': {'token': 't'}})
            if MODE == 'badframe':
                with out_lock:
                    sys.stdout.buffer.write(b'Content-Length: abc\r\n\r\n')
                    sys.stdout.buffer.flush()
        elif method == 'textDocument/didOpen':
            docs[params['textDocument']['uri']] = params['textDocument']['text']
            publish(params['textDocument']['uri'])
        elif method == 'textDocument/didChange':
            docs[params['textDocument']['uri']] = params['contentChanges'][-1]['text']
            publish(params['textDocument']['uri'])
        elif method == 'exit':
            if MODE == 'noshutdown':
                continue
            return


if __name__ == '__main__':
    main()
