import React, { useEffect, useRef, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { VscAdd, VscClose, VscTrash } from 'react-icons/vsc';
import api from '../services/api';

const TerminalInstance = ({ isActive, onResize, onTerminalReady, theme, onDirectoryChange, initialDirectory }) => {
  const terminalRef = useRef(null);
  const xtermRef = useRef(null);
  const fitAddonRef = useRef(null);
  const commandHistoryRef = useRef([]);
  const historyIndexRef = useRef(-1);
  const bufferRef = useRef('');
  const currentDirRef = useRef('~'); // Display directory (may contain ~)
  const actualDirRef = useRef(''); // Actual directory path
  const homeDirRef = useRef(''); // Home directory path
  const usernameRef = useRef('user');

  // Initialize terminal session info
  const initSessionInfo = async () => {
    try {
      // Get username
      const whoamiResult = await api.executeCommand('whoami');
      if (whoamiResult.stdout) {
        usernameRef.current = whoamiResult.stdout.trim();
      }

      // Get home directory
      const homeResult = await api.executeCommand('echo $HOME');
      if (homeResult.stdout) {
        homeDirRef.current = homeResult.stdout.trim();
      }

      // Get current directory
      const pwdResult = await api.executeCommand('pwd');
      if (pwdResult.stdout) {
        const dir = pwdResult.stdout.trim();
        actualDirRef.current = dir;
        const shortDir = dir.replace(homeDirRef.current, '~');
        currentDirRef.current = shortDir;
      }
    } catch (err) {
      console.error('Failed to get session info:', err);
    }
  };

  useEffect(() => {
    if (!terminalRef.current) return;

    // Initialize terminal
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: theme === 'dark' ? {
        background: '#1e1e1e',
        foreground: '#cccccc',
        cursor: '#cccccc',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#e5e5e5'
      } : {
        background: '#ffffff',
        foreground: '#333333',
        cursor: '#333333',
        black: '#000000',
        red: '#cd3131',
        green: '#00bc00',
        yellow: '#949800',
        blue: '#0451a5',
        magenta: '#bc05bc',
        cyan: '#0598bc',
        white: '#555555',
        brightBlack: '#666666',
        brightRed: '#cd3131',
        brightGreen: '#14ce14',
        brightYellow: '#b5ba00',
        brightBlue: '#0451a5',
        brightMagenta: '#bc05bc',
        brightCyan: '#0598bc',
        brightWhite: '#a5a5a5'
      },
      scrollback: 1000,
      convertEol: true,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalRef.current);

    // Fit terminal immediately and after a delay
    fitAddon.fit();
    setTimeout(() => {
      fitAddon.fit();
    }, 100);

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    // Expose terminal instance to parent
    if (onTerminalReady) {
      onTerminalReady({ term, writePrompt: () => writePrompt(term) });
    }

    // Initialize session info
    initSessionInfo().then(async () => {
      // If initialDirectory is provided, cd to it
      if (initialDirectory && initialDirectory !== actualDirRef.current) {
        try {
          const result = await api.executeCommand(`cd "${initialDirectory}" && pwd`);
          if (result.stdout) {
            const dir = result.stdout.trim();
            actualDirRef.current = dir;
            const shortDir = dir.replace(homeDirRef.current, '~');
            currentDirRef.current = shortDir;
          }
        } catch (err) {
          console.error('Failed to cd to initial directory:', err);
        }
      }

      // Notify parent about initial directory
      if (onDirectoryChange && actualDirRef.current) {
        onDirectoryChange(actualDirRef.current);
      }

      // Welcome message
      term.writeln('Litterbox Terminal');
      term.writeln('Type commands and press Enter to execute');
      term.writeln('');
      writePrompt(term);
    });

    // Handle terminal input
    term.onData((data) => {
      const code = data.charCodeAt(0);

      if (code === 13) {
        // Enter key
        term.write('\r\n');
        if (bufferRef.current.trim()) {
          executeCommand(term, bufferRef.current.trim());
          commandHistoryRef.current.push(bufferRef.current.trim());
          historyIndexRef.current = commandHistoryRef.current.length;
        } else {
          writePrompt(term);
        }
        bufferRef.current = '';
      } else if (code === 127) {
        // Backspace
        if (bufferRef.current.length > 0) {
          bufferRef.current = bufferRef.current.slice(0, -1);
          term.write('\b \b');
        }
      } else if (code === 27) {
        // Escape sequences (arrow keys)
        if (data === '\x1b[A') {
          // Up arrow - previous command
          if (historyIndexRef.current > 0) {
            term.write('\r\x1b[K');
            writePrompt(term);
            historyIndexRef.current--;
            bufferRef.current = commandHistoryRef.current[historyIndexRef.current] || '';
            term.write(bufferRef.current);
          }
        } else if (data === '\x1b[B') {
          // Down arrow - next command
          if (historyIndexRef.current < commandHistoryRef.current.length - 1) {
            term.write('\r\x1b[K');
            writePrompt(term);
            historyIndexRef.current++;
            bufferRef.current = commandHistoryRef.current[historyIndexRef.current] || '';
            term.write(bufferRef.current);
          } else if (historyIndexRef.current === commandHistoryRef.current.length - 1) {
            term.write('\r\x1b[K');
            writePrompt(term);
            historyIndexRef.current = commandHistoryRef.current.length;
            bufferRef.current = '';
          }
        }
      } else if (code >= 32) {
        // Printable characters
        bufferRef.current += data;
        term.write(data);
      }
    });

    return () => {
      term.dispose();
    };
  }, []);

  // Handle resize when terminal becomes active or size changes
  useEffect(() => {
    if (isActive && fitAddonRef.current) {
      setTimeout(() => {
        fitAddonRef.current.fit();
      }, 0);
    }
  }, [isActive, onResize]);

  // Update theme when it changes
  useEffect(() => {
    if (xtermRef.current) {
      const newTheme = theme === 'dark' ? {
        background: '#1e1e1e',
        foreground: '#cccccc',
        cursor: '#cccccc',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#e5e5e5'
      } : {
        background: '#ffffff',
        foreground: '#333333',
        cursor: '#333333',
        black: '#000000',
        red: '#cd3131',
        green: '#00bc00',
        yellow: '#949800',
        blue: '#0451a5',
        magenta: '#bc05bc',
        cyan: '#0598bc',
        white: '#555555',
        brightBlack: '#666666',
        brightRed: '#cd3131',
        brightGreen: '#14ce14',
        brightYellow: '#b5ba00',
        brightBlue: '#0451a5',
        brightMagenta: '#bc05bc',
        brightCyan: '#0598bc',
        brightWhite: '#a5a5a5'
      };
      xtermRef.current.options.theme = newTheme;
    }
  }, [theme]);

  const writePrompt = (term) => {
    // Format: user@litterbox:~/path $
    const user = usernameRef.current;
    const dir = currentDirRef.current;
    term.write(`\r\n\x1b[32m${user}@litterbox\x1b[0m:\x1b[34m${dir}\x1b[0m$ `);
  };

  const executeCommand = async (term, command) => {
    try {
      // Use actual directory path for execution
      const cwd = actualDirRef.current;

      // For cd command, we need to handle it specially
      if (command.trim().startsWith('cd ') || command.trim() === 'cd') {
        // Execute cd command with pwd to get new directory in one shell session
        const cdAndPwd = `${command} && pwd`;
        const result = await api.executeCommand(cdAndPwd, cwd);

        if (result.exit_code === 0 && result.stdout) {
          const dir = result.stdout.trim();
          actualDirRef.current = dir;
          // Display shortened path with ~
          const shortDir = dir.replace(homeDirRef.current, '~');
          currentDirRef.current = shortDir;

          // Notify parent about directory change
          if (onDirectoryChange) {
            onDirectoryChange(dir);
          }
        } else if (result.stderr) {
          term.write('\x1b[31m' + result.stderr.replace(/\n/g, '\r\n') + '\x1b[0m');
        }
      } else {
        // Regular command execution with current working directory
        const result = await api.executeCommand(command, cwd);

        if (result.stdout) {
          term.write(result.stdout.replace(/\n/g, '\r\n'));
        }

        if (result.stderr) {
          term.write('\x1b[31m' + result.stderr.replace(/\n/g, '\r\n') + '\x1b[0m');
        }

        if (result.exit_code !== 0) {
          term.write(`\x1b[31m[Exit code: ${result.exit_code}]\x1b[0m\r\n`);
        }
      }
    } catch (err) {
      term.write(`\x1b[31mError: ${err.message}\x1b[0m\r\n`);
    }

    writePrompt(term);
  };

  return (
    <div
      ref={terminalRef}
      style={{
        height: '100%',
        width: '100%',
        display: isActive ? 'block' : 'none'
      }}
    />
  );
};

const Terminal = ({ style, theme, onDirectoryChange, initialDirectory }) => {
  const [terminals, setTerminals] = useState([{ id: 1, name: 'Terminal 1' }]);
  const [activeTerminalId, setActiveTerminalId] = useState(1);
  const [nextId, setNextId] = useState(2);
  const resizeKeyRef = useRef(0);
  const terminalInstancesRef = useRef({});
  const terminalDirectoriesRef = useRef({ 1: initialDirectory || '' }); // Store directory for each terminal
  const initialDirectoryRef = useRef(initialDirectory);

  const createNewTerminal = () => {
    const newTerminal = {
      id: nextId,
      name: `Terminal ${nextId}`
    };
    setTerminals([...terminals, newTerminal]);
    terminalDirectoriesRef.current[nextId] = ''; // Initialize directory for new terminal
    setActiveTerminalId(nextId);
    setNextId(nextId + 1);
  };

  const closeTerminal = (id, event) => {
    event?.stopPropagation();

    if (terminals.length === 1) {
      return; // Don't close the last terminal
    }

    const index = terminals.findIndex(t => t.id === id);
    const newTerminals = terminals.filter(t => t.id !== id);

    setTerminals(newTerminals);

    // Remove terminal instance reference and directory
    delete terminalInstancesRef.current[id];
    delete terminalDirectoriesRef.current[id];

    // If closing active terminal, switch to another one
    if (activeTerminalId === id) {
      const newActiveIndex = Math.max(0, index - 1);
      const newActiveId = newTerminals[newActiveIndex].id;
      setActiveTerminalId(newActiveId);

      // Notify about directory change when switching terminal
      if (onDirectoryChange && terminalDirectoriesRef.current[newActiveId]) {
        onDirectoryChange(terminalDirectoriesRef.current[newActiveId]);
      }
    }
  };

  const clearActiveTerminal = () => {
    const termInstance = terminalInstancesRef.current[activeTerminalId];
    if (termInstance) {
      termInstance.term.clear();
      termInstance.writePrompt();
    }
  };

  const handleTerminalReady = (id, instance) => {
    terminalInstancesRef.current[id] = instance;
  };

  const handleDirectoryChange = (id, directory) => {
    terminalDirectoriesRef.current[id] = directory;

    // Only notify parent if this is the active terminal
    if (id === activeTerminalId && onDirectoryChange) {
      onDirectoryChange(directory);
    }
  };

  // Handle switching between terminals
  const handleTerminalSwitch = (id) => {
    setActiveTerminalId(id);

    // Notify about the directory of the switched-to terminal
    const dir = terminalDirectoriesRef.current[id];
    console.log('Terminal switch:', id, 'Directory:', dir);
    if (onDirectoryChange && dir) {
      console.log('Calling onDirectoryChange with:', dir);
      onDirectoryChange(dir);
    }
  };

  // Trigger resize on active terminal when component size changes
  useEffect(() => {
    const resizeObserver = new ResizeObserver(() => {
      resizeKeyRef.current += 1;
    });

    const terminalContainer = document.querySelector('.terminal-wrapper');
    if (terminalContainer) {
      resizeObserver.observe(terminalContainer);
    }

    return () => resizeObserver.disconnect();
  }, []);

  return (
    <div className="terminal-container" style={style}>
      <div className="terminal-header">
        <div className="terminal-tabs">
          {terminals.map(terminal => (
            <div
              key={terminal.id}
              className={`terminal-tab ${activeTerminalId === terminal.id ? 'active' : ''}`}
              onClick={() => handleTerminalSwitch(terminal.id)}
            >
              <span>{terminal.name}</span>
              {terminals.length > 1 && (
                <button
                  className="close-btn"
                  onClick={(e) => closeTerminal(terminal.id, e)}
                >
                  <VscClose size={14} />
                </button>
              )}
            </div>
          ))}
        </div>
        <div className="terminal-actions">
          <button
            className="btn btn-icon btn-small"
            onClick={clearActiveTerminal}
            title="Clear Terminal"
          >
            <VscTrash />
          </button>
          <button
            className="btn btn-icon btn-small"
            onClick={createNewTerminal}
            title="New Terminal"
          >
            <VscAdd />
          </button>
        </div>
      </div>
      <div className="terminal-wrapper">
        {terminals.map(terminal => (
          <TerminalInstance
            key={terminal.id}
            isActive={activeTerminalId === terminal.id}
            onResize={resizeKeyRef.current}
            onTerminalReady={(instance) => handleTerminalReady(terminal.id, instance)}
            theme={theme}
            onDirectoryChange={(dir) => handleDirectoryChange(terminal.id, dir)}
            initialDirectory={terminal.id === 1 ? initialDirectoryRef.current : null}
          />
        ))}
      </div>
    </div>
  );
};

export default Terminal;
