import React, { useState, useEffect, useRef } from 'react';
import FileTree from './components/FileTree';
import Editor from './components/Editor';
import Terminal from './components/Terminal';
import AuthModal from './components/AuthModal';
import api from './services/api';
import './App.css';

function App() {
  const [authenticated, setAuthenticated] = useState(false);
  const [authChecked, setAuthChecked] = useState(false);
  const [activeFile, setActiveFile] = useState(null);
  const [sidebarWidth, setSidebarWidth] = useState(250);
  const [terminalHeight, setTerminalHeight] = useState(200);
  const [currentDirectory, setCurrentDirectory] = useState('');
  const [theme, setTheme] = useState(() => {
    // Load theme from localStorage or default to dark
    return localStorage.getItem('theme') || 'dark';
  });
  const editorRef = useRef(null);
  const isResizingSidebar = useRef(false);
  const isResizingTerminal = useRef(false);
  const initialDirectoryRef = useRef(null);

  // Get token from URL parameter
  const getTokenFromURL = () => {
    const params = new URLSearchParams(window.location.search);
    return params.get('token');
  };

  // Get file path from URL parameter
  const getFileFromURL = () => {
    const params = new URLSearchParams(window.location.search);
    return params.get('file');
  };

  // Get directory from URL parameter
  const getDirectoryFromURL = () => {
    const params = new URLSearchParams(window.location.search);
    return params.get('dir');
  };

  // Redirect to auth page without token
  const redirectToAuth = () => {
    const currentPath = window.location.pathname;
    if (currentPath !== '/' || window.location.search) {
      // Clear token and redirect to home
      window.history.replaceState({}, '', '/');
      window.location.reload();
    }
  };

  useEffect(() => {
    const checkAuth = async () => {
      // Check if token exists in URL
      const token = getTokenFromURL();

      if (token) {
        api.setToken(token);
      }

      // Set up auth error callback
      api.setAuthErrorCallback(() => {
        setAuthenticated(false);
        redirectToAuth();
      });

      // Try to make a simple API call to check if auth is needed
      try {
        await api.healthCheck();
        // Health check succeeded, we're authenticated (or no auth needed)
        setAuthenticated(true);

        // Check if there's a directory parameter to set
        let dirToSet = getDirectoryFromURL();

        // If no URL parameter, get default directory from config
        if (!dirToSet) {
          try {
            const config = await api.getConfig();
            if (config.default_directory) {
              dirToSet = config.default_directory;
            }
          } catch (err) {
            console.error('Failed to get config:', err);
          }
        }

        console.log('URL dir parameter:', dirToSet);
        if (dirToSet) {
          console.log('Setting currentDirectory to:', dirToSet);
          initialDirectoryRef.current = dirToSet;
          setCurrentDirectory(dirToSet);
        }

        // Check if there's a file parameter to open
        const fileToOpen = getFileFromURL();
        if (fileToOpen) {
          // Delay to ensure file tree is loaded first
          setTimeout(() => {
            setActiveFile(fileToOpen);
          }, 500);
        }
      } catch (err) {
        // If health check failed, we might need auth
        if (!token) {
          setAuthenticated(false);
        }
      } finally {
        setAuthChecked(true);
      }
    };

    checkAuth();
  }, []);

  const handleAuthenticated = (token) => {
    // Redirect to main page with token in URL
    const url = new URL(window.location.href);
    url.searchParams.set('token', token);
    window.location.href = url.toString();
  };

  const handleFileClick = (path) => {
    setActiveFile(path);
  };

  const handleDirectoryChange = (directory) => {
    console.log('handleDirectoryChange called with:', directory);
    console.log('Current directory before:', currentDirectory);
    setCurrentDirectory(directory);
  };

  // Function to refresh config and update directory
  const refreshConfig = async () => {
    try {
      const config = await api.getConfig();
      if (config.default_directory) {
        console.log('Refreshed config, new directory:', config.default_directory);
        setCurrentDirectory(config.default_directory);
      }
    } catch (err) {
      console.error('Failed to refresh config:', err);
    }
  };

  // Expose refreshConfig globally so it can be called from anywhere
  useEffect(() => {
    window.refreshConfig = refreshConfig;
    return () => {
      delete window.refreshConfig;
    };
  }, []);

  const toggleTheme = () => {
    const newTheme = theme === 'dark' ? 'light' : 'dark';
    setTheme(newTheme);
    localStorage.setItem('theme', newTheme);
  };

  // Apply theme to document
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
  }, [theme]);

  // Sidebar resize
  useEffect(() => {
    const handleMouseMove = (e) => {
      if (isResizingSidebar.current) {
        const newWidth = e.clientX;
        if (newWidth >= 150 && newWidth <= 500) {
          setSidebarWidth(newWidth);
        }
      }

      if (isResizingTerminal.current) {
        const newHeight = window.innerHeight - e.clientY;
        if (newHeight >= 100 && newHeight <= 600) {
          setTerminalHeight(newHeight);
        }
      }
    };

    const handleMouseUp = () => {
      isResizingSidebar.current = false;
      isResizingTerminal.current = false;
      document.body.style.cursor = 'default';
      document.body.style.userSelect = 'auto';
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
    };
  }, []);

  const handleSidebarResizeStart = () => {
    isResizingSidebar.current = true;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  };

  const handleTerminalResizeStart = () => {
    isResizingTerminal.current = true;
    document.body.style.cursor = 'row-resize';
    document.body.style.userSelect = 'none';
  };

  // Show loading or auth modal
  if (!authChecked) {
    return <div className="loading">Loading...</div>;
  }

  if (!authenticated) {
    return <AuthModal onAuthenticated={handleAuthenticated} />;
  }

  return (
    <div className="app">
      {/* Main Container */}
      <div className="main-container">
        {/* Sidebar */}
        <div className="sidebar" style={{ width: sidebarWidth }}>
          <FileTree
            onFileClick={handleFileClick}
            activeFile={activeFile}
            theme={theme}
            onToggleTheme={toggleTheme}
            rootPath={currentDirectory}
          />
        </div>

        {/* Sidebar Resizer */}
        <div
          className="resizer"
          onMouseDown={handleSidebarResizeStart}
        />

        {/* Content Area */}
        <div className="content-area">
          {/* Editor */}
          <Editor
            ref={editorRef}
            activeFile={activeFile}
            theme={theme}
            onFileClose={(path) => {
              if (activeFile === path) {
                setActiveFile(null);
              }
            }}
          />

          {/* Terminal Resizer */}
          <div
            className="resizer horizontal"
            onMouseDown={handleTerminalResizeStart}
          />

          {/* Terminal */}
          <Terminal
            style={{ height: terminalHeight }}
            theme={theme}
            onDirectoryChange={handleDirectoryChange}
            initialDirectory={initialDirectoryRef.current}
          />
        </div>
      </div>
    </div>
  );
}

export default App;
