import React, { useState, useEffect, useRef } from 'react';
import {
  VscFolder,
  VscFolderOpened,
  VscFile,
  VscChevronRight,
  VscRefresh,
  VscColorMode,
  VscNewFile,
  VscNewFolder,
  VscTrash,
  VscCloudDownload,
  VscListSelection
} from 'react-icons/vsc';
import api from '../services/api';

const FileTreeItem = ({ node, level, onFileClick, activeFile, expandedFolders, toggleFolder, onContextMenu, showFileSize }) => {
  const isExpanded = expandedFolders.has(node.path);
  const isActive = activeFile === node.path;

  const handleClick = () => {
    if (node.is_dir) {
      toggleFolder(node.path);
    } else {
      onFileClick(node.path);
    }
  };

  const handleContextMenu = (e) => {
    e.preventDefault();
    e.stopPropagation();
    onContextMenu(e, node);
  };

  const formatFileSize = (bytes) => {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes}B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}K`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)}M`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)}G`;
  };

  return (
    <>
      <div
        className={`file-tree-item ${node.is_dir ? 'folder' : ''} ${isActive ? 'active' : ''} ${isExpanded ? 'expanded' : ''}`}
        data-level={level}
        onClick={handleClick}
        onContextMenu={handleContextMenu}
      >
        <span className="file-tree-item-content">
          {node.is_dir && (
            <VscChevronRight className="expand-icon" />
          )}
          {node.is_dir ? (
            isExpanded ? <VscFolderOpened /> : <VscFolder />
          ) : (
            <VscFile />
          )}
          <span className="file-name">{node.name}</span>
        </span>
        {showFileSize && !node.is_dir && node.size !== undefined && (
          <span className="file-size">{formatFileSize(node.size)}</span>
        )}
      </div>
      {node.is_dir && isExpanded && node.children && (
        node.children.map((child) => (
          <FileTreeItem
            key={child.path}
            node={child}
            level={level + 1}
            onFileClick={onFileClick}
            activeFile={activeFile}
            expandedFolders={expandedFolders}
            toggleFolder={toggleFolder}
            onContextMenu={onContextMenu}
            showFileSize={showFileSize}
          />
        ))
      )}
    </>
  );
};

const FileTree = ({ onFileClick, activeFile, onRefresh, theme, onToggleTheme, rootPath }) => {
  const [tree, setTree] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [expandedFolders, setExpandedFolders] = useState(new Set());
  const [contextMenu, setContextMenu] = useState(null);
  const [inputDialog, setInputDialog] = useState(null);
  const [showFileSize, setShowFileSize] = useState(() => {
    // Load from localStorage or default to true
    const saved = localStorage.getItem('showFileSize');
    return saved === null ? true : saved === 'true';
  });
  const contextMenuRef = useRef(null);

  const toggleFileSize = () => {
    const newValue = !showFileSize;
    setShowFileSize(newValue);
    localStorage.setItem('showFileSize', newValue.toString());
  };

  const loadFileTree = async (path) => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.getFileTree(path || '');
      setTree(data);
      // Auto-expand root
      if (data) {
        setExpandedFolders(new Set([data.path]));
      }
    } catch (err) {
      setError(err.message);
      console.error('Failed to load file tree:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    console.log('FileTree rootPath changed to:', rootPath);
    setExpandedFolders(new Set()); // Clear expanded folders when directory changes
    loadFileTree(rootPath);
  }, [rootPath]);

  useEffect(() => {
    const handleClickOutside = (e) => {
      if (contextMenuRef.current && !contextMenuRef.current.contains(e.target)) {
        setContextMenu(null);
      }
    };

    if (contextMenu) {
      document.addEventListener('click', handleClickOutside);
      return () => document.removeEventListener('click', handleClickOutside);
    }
  }, [contextMenu]);

  const handleRefresh = () => {
    loadFileTree(rootPath);
    if (onRefresh) {
      onRefresh();
    }
  };

  const handleContextMenu = (e, node) => {
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      node: node
    });
  };

  const handleNewFile = (parentPath) => {
    setInputDialog({
      title: 'New File',
      placeholder: 'filename.ext',
      isDir: false,
      parentPath: parentPath
    });
    setContextMenu(null);
  };

  const handleNewFolder = (parentPath) => {
    setInputDialog({
      title: 'New Folder',
      placeholder: 'folder-name',
      isDir: true,
      parentPath: parentPath
    });
    setContextMenu(null);
  };

  const handleDelete = async (path, isDir) => {
    const confirmed = window.confirm(`Are you sure you want to delete this ${isDir ? 'folder' : 'file'}?`);
    if (!confirmed) return;

    try {
      await api.deleteFile(path);
      loadFileTree(rootPath);
      if (onRefresh) onRefresh();
    } catch (err) {
      alert('Failed to delete: ' + err.message);
    }
    setContextMenu(null);
  };

  const handleDownload = async (path) => {
    try {
      await api.downloadFile(path);
    } catch (err) {
      alert('Failed to download: ' + err.message);
    }
    setContextMenu(null);
  };

  const handleInputSubmit = async (name) => {
    if (!name.trim()) return;

    const fullPath = `${inputDialog.parentPath}/${name}`;

    try {
      await api.createFile(fullPath, inputDialog.isDir);
      loadFileTree(rootPath);
      if (onRefresh) onRefresh();
      setInputDialog(null);
    } catch (err) {
      alert('Failed to create: ' + err.message);
    }
  };

  const toggleFolder = (path) => {
    setExpandedFolders((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  };

  if (loading) {
    return (
      <div className="file-tree">
        <div className="loading">Loading files...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="file-tree">
        <div className="error-message">Error: {error}</div>
        <button className="btn btn-small" onClick={() => loadFileTree(rootPath)}>
          Retry
        </button>
      </div>
    );
  }

  if (!tree) {
    return (
      <div className="file-tree">
        <div className="loading">No files found</div>
      </div>
    );
  }

  return (
    <>
      <div className="sidebar-header">
        <h3>Files</h3>
        <div className="sidebar-actions">
          <button
            className="btn btn-icon"
            onClick={toggleFileSize}
            title={showFileSize ? "Hide File Sizes" : "Show File Sizes"}
          >
            <VscListSelection />
          </button>
          <button
            className="btn btn-icon"
            onClick={onToggleTheme}
            title={`Switch to ${theme === 'dark' ? 'Light' : 'Dark'} Mode`}
          >
            <VscColorMode />
          </button>
          <button
            className="btn btn-icon"
            onClick={handleRefresh}
            title="Refresh File Tree"
          >
            <VscRefresh />
          </button>
        </div>
      </div>
      <div className="file-tree">
        <FileTreeItem
          node={tree}
          level={0}
          onFileClick={onFileClick}
          activeFile={activeFile}
          expandedFolders={expandedFolders}
          toggleFolder={toggleFolder}
          onContextMenu={handleContextMenu}
          showFileSize={showFileSize}
        />
      </div>

      {/* Context Menu */}
      {contextMenu && (
        <div
          ref={contextMenuRef}
          className="context-menu"
          style={{ left: contextMenu.x, top: contextMenu.y }}
        >
          {contextMenu.node.is_dir && (
            <>
              <div className="context-menu-item" onClick={() => handleNewFile(contextMenu.node.path)}>
                <VscNewFile /> New File
              </div>
              <div className="context-menu-item" onClick={() => handleNewFolder(contextMenu.node.path)}>
                <VscNewFolder /> New Folder
              </div>
              <div className="context-menu-divider" />
            </>
          )}
          {!contextMenu.node.is_dir && (
            <>
              <div className="context-menu-item" onClick={() => handleDownload(contextMenu.node.path)}>
                <VscCloudDownload /> Download
              </div>
              <div className="context-menu-divider" />
            </>
          )}
          <div className="context-menu-item danger" onClick={() => handleDelete(contextMenu.node.path, contextMenu.node.is_dir)}>
            <VscTrash /> Delete
          </div>
        </div>
      )}

      {/* Input Dialog */}
      {inputDialog && (
        <div className="input-dialog" onClick={() => setInputDialog(null)}>
          <div className="input-dialog-content" onClick={(e) => e.stopPropagation()}>
            <h3>{inputDialog.title}</h3>
            <input
              type="text"
              placeholder={inputDialog.placeholder}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  handleInputSubmit(e.target.value);
                } else if (e.key === 'Escape') {
                  setInputDialog(null);
                }
              }}
            />
            <div className="input-dialog-buttons">
              <button className="secondary" onClick={() => setInputDialog(null)}>
                Cancel
              </button>
              <button
                className="primary"
                onClick={(e) => {
                  const input = e.target.closest('.input-dialog-content').querySelector('input');
                  handleInputSubmit(input.value);
                }}
              >
                Create
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default FileTree;
