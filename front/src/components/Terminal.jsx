import React, { useEffect, useRef, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { VscAdd, VscClose, VscTrash } from 'react-icons/vsc';
import api from '../services/api';

const TerminalInstance = ({ isActive, onResize, onTerminalReady, theme }) => {
  const terminalRef = useRef(null);
  const xtermRef = useRef(null);
  const fitAddonRef = useRef(null);
  const wsRef = useRef(null);

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
      onTerminalReady({ term, clear: () => term.clear() });
    }

    // Connect to WebSocket
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const token = api.getToken();
    const wsUrl = `${protocol}//${host}/ws/terminal?rows=${term.rows}&cols=${term.cols}${token ? `&token=${token}` : ''}`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      console.log('WebSocket connected');

      // Send data from terminal to WebSocket
      term.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(data);
        }
      });

      // Handle terminal resize
      term.onResize(({ rows, cols }) => {
        if (ws.readyState === WebSocket.OPEN) {
          // Send resize message: [1, rows_high, rows_low, cols_high, cols_low]
          const resizeMsg = new Uint8Array([
            1,
            (rows >> 8) & 0xFF,
            rows & 0xFF,
            (cols >> 8) & 0xFF,
            cols & 0xFF
          ]);
          ws.send(resizeMsg);
        }
      });
    };

    ws.onmessage = (event) => {
      // Write data from WebSocket to terminal
      if (event.data instanceof Blob) {
        event.data.arrayBuffer().then(buffer => {
          const uint8Array = new Uint8Array(buffer);
          term.write(uint8Array);
        });
      } else {
        term.write(event.data);
      }
    };

    ws.onerror = (error) => {
      console.error('WebSocket error:', error);
      term.write('\r\n\x1b[31mWebSocket connection error\x1b[0m\r\n');
    };

    ws.onclose = () => {
      console.log('WebSocket disconnected');
      term.write('\r\n\x1b[33mConnection closed\x1b[0m\r\n');
    };

    return () => {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close();
      }
      term.dispose();
    };
  }, []);

  // Handle resize when terminal becomes active or size changes
  useEffect(() => {
    if (isActive && fitAddonRef.current && xtermRef.current) {
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

const Terminal = ({ style, theme }) => {
  const [terminals, setTerminals] = useState([{ id: 1, name: 'Terminal 1' }]);
  const [activeTerminalId, setActiveTerminalId] = useState(1);
  const [nextId, setNextId] = useState(2);
  const resizeKeyRef = useRef(0);
  const terminalInstancesRef = useRef({});

  const createNewTerminal = () => {
    const newTerminal = {
      id: nextId,
      name: `Terminal ${nextId}`
    };
    setTerminals([...terminals, newTerminal]);
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

    // Remove terminal instance reference
    delete terminalInstancesRef.current[id];

    // If closing active terminal, switch to another one
    if (activeTerminalId === id) {
      const newActiveIndex = Math.max(0, index - 1);
      const newActiveId = newTerminals[newActiveIndex].id;
      setActiveTerminalId(newActiveId);
    }
  };

  const clearActiveTerminal = () => {
    const termInstance = terminalInstancesRef.current[activeTerminalId];
    if (termInstance) {
      termInstance.clear();
    }
  };

  const handleTerminalReady = (id, instance) => {
    terminalInstancesRef.current[id] = instance;
  };

  const handleTerminalSwitch = (id) => {
    setActiveTerminalId(id);
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
          />
        ))}
      </div>
    </div>
  );
};

export default Terminal;
