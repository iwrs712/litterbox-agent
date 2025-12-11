import React, { useState, useEffect, useRef } from 'react';
import MonacoEditor from '@monaco-editor/react';
import { VscClose, VscCircleFilled, VscCloudDownload, VscSave } from 'react-icons/vsc';
import api from '../services/api';

// File types that should be edited in Monaco
const EDITABLE_LANGUAGES = [
  'javascript', 'typescript', 'python', 'java', 'go', 'rust', 'c', 'cpp',
  'csharp', 'php', 'ruby', 'swift', 'kotlin', 'scala', 'html', 'css',
  'scss', 'less', 'json', 'xml', 'yaml', 'sql', 'shell', 'markdown',
  'plaintext', 'dockerfile', 'makefile', 'ini', 'toml', 'vue', 'svelte'
];

// Image file extensions
const IMAGE_EXTENSIONS = ['.png', '.jpg', '.jpeg', '.gif', '.bmp', '.svg', '.webp', '.ico'];

// Binary file extensions that should not be edited
const BINARY_EXTENSIONS = [
  '.zip', '.tar', '.gz', '.rar', '.7z', '.bz2', '.xz',
  '.exe', '.dll', '.so', '.dylib', '.bin',
  '.pdf', '.doc', '.docx', '.xls', '.xlsx', '.ppt', '.pptx',
  '.mp3', '.mp4', '.avi', '.mov', '.mkv', '.flv', '.wmv',
  '.ttf', '.otf', '.woff', '.woff2', '.eot',
  '.class', '.jar', '.war', '.pyc', '.o', '.obj',
  '.db', '.sqlite', '.sqlite3'
];

const Editor = ({ activeFile, onFileClose, theme }) => {
  const [openFiles, setOpenFiles] = useState([]);
  const [activeTab, setActiveTab] = useState(null);
  const [fileContents, setFileContents] = useState({});
  const [originalContents, setOriginalContents] = useState({}); // Store original content for comparison
  const [fileMetadata, setFileMetadata] = useState({});
  const [modifiedFiles, setModifiedFiles] = useState(new Set());
  const editorRef = useRef(null);

  // Load file content when activeFile changes
  useEffect(() => {
    if (activeFile && !openFiles.find(f => f.path === activeFile)) {
      loadFile(activeFile);
    } else if (activeFile) {
      setActiveTab(activeFile);
    }
  }, [activeFile]);

  const isImageFile = (path) => {
    return IMAGE_EXTENSIONS.some(ext => path.toLowerCase().endsWith(ext));
  };

  const isBinaryFile = (path) => {
    return BINARY_EXTENSIONS.some(ext => path.toLowerCase().endsWith(ext));
  };

  const isEditableFile = (language, path) => {
    // Check if it's a known binary file extension
    if (isBinaryFile(path)) {
      return false;
    }

    // If backend returned plaintext language, it means the file is editable
    // (backend already checked file size and content)
    return EDITABLE_LANGUAGES.includes(language);
  };

  const formatFileSize = (bytes) => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
  };

  const loadFile = async (path) => {
    try {
      const fileName = path.split('/').pop();
      const isImage = isImageFile(path);
      const isBinary = isBinaryFile(path);

      let content = '';
      let language = 'plaintext';
      let fileSize = 0;
      let modTime = '';
      let isEditable = false;

      if (isBinary && !isImage) {
        // Known binary file extension - mark as binary, no content
        isEditable = false;
        language = 'binary';
        fileSize = 0; // Will show download button
        modTime = '';
      } else {
        // Try to load content - backend will determine if it's editable
        try {
          const data = await api.getFileContent(path);
          content = data.content;
          language = data.language;
          fileSize = data.size || new Blob([content]).size;
          modTime = data.mod_time || '';
          isEditable = isEditableFile(language, path);
        } catch (err) {
          // If backend rejects (binary file), treat as binary
          isEditable = false;
          language = 'binary';
          fileSize = 0;
          modTime = '';
        }
      }

      setOpenFiles(prev => [...prev, {
        path,
        name: fileName,
        language,
        isImage,
        isEditable
      }]);

      if (isEditable || isImage) {
        setFileContents(prev => ({ ...prev, [path]: content }));
        setOriginalContents(prev => ({ ...prev, [path]: content })); // Store original content
      }

      setFileMetadata(prev => ({ ...prev, [path]: { size: fileSize, modTime } }));
      setActiveTab(path);
    } catch (err) {
      console.error('Failed to load file:', err);
      alert(`Failed to load file: ${err.message}`);
    }
  };

  const closeFile = (path, event) => {
    event?.stopPropagation();

    if (modifiedFiles.has(path)) {
      if (!window.confirm('File has unsaved changes. Close anyway?')) {
        return;
      }
    }

    setOpenFiles(prev => {
      const filtered = prev.filter(f => f.path !== path);
      if (activeTab === path && filtered.length > 0) {
        setActiveTab(filtered[filtered.length - 1].path);
      } else if (filtered.length === 0) {
        setActiveTab(null);
      }
      return filtered;
    });

    setFileContents(prev => {
      const next = { ...prev };
      delete next[path];
      return next;
    });

    setOriginalContents(prev => {
      const next = { ...prev };
      delete next[path];
      return next;
    });

    setModifiedFiles(prev => {
      const next = new Set(prev);
      next.delete(path);
      return next;
    });

    if (onFileClose) {
      onFileClose(path);
    }
  };

  const handleEditorChange = (value) => {
    if (activeTab) {
      setFileContents(prev => ({ ...prev, [activeTab]: value }));

      // Check if content is different from original
      const originalContent = originalContents[activeTab];
      if (value !== originalContent) {
        setModifiedFiles(prev => new Set(prev).add(activeTab));
      } else {
        // Content matches original, remove from modified files
        setModifiedFiles(prev => {
          const next = new Set(prev);
          next.delete(activeTab);
          return next;
        });
      }
    }
  };

  const saveFile = async (path) => {
    try {
      const content = fileContents[path];
      await api.saveFile(path, content);

      // Update original content after successful save
      setOriginalContents(prev => ({ ...prev, [path]: content }));

      setModifiedFiles(prev => {
        const next = new Set(prev);
        next.delete(path);
        return next;
      });
      console.log('File saved:', path);
    } catch (err) {
      console.error('Failed to save file:', err);
      alert(`Failed to save file: ${err.message}`);
    }
  };

  const saveCurrentFile = () => {
    if (activeTab) {
      saveFile(activeTab);
    }
  };

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e) => {
      // Ctrl+S or Cmd+S to save
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        saveCurrentFile();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activeTab, fileContents]);

  const handleEditorDidMount = (editor) => {
    editorRef.current = editor;
  };

  const handleDownload = async (path) => {
    try {
      await api.downloadFile(path);
    } catch (err) {
      console.error('Download failed:', err);
      alert(`Download failed: ${err.message}`);
    }
  };

  const activeFileData = openFiles.find(f => f.path === activeTab);
  const metadata = activeTab ? fileMetadata[activeTab] : null;

  // File Preview Component
  const FilePreview = ({ file, content, metadata }) => {
    if (file.isImage) {
      // Image preview - content is already base64 encoded from backend
      const imageUrl = `data:image/${file.path.split('.').pop()};base64,${content}`;
      return (
        <div className="file-preview">
          <div className="file-preview-header">
            <div className="file-info">
              <h3>{file.name}</h3>
              <p>Type: Image • Size: {formatFileSize(metadata?.size || 0)}{metadata?.modTime ? ` • Modified: ${metadata.modTime}` : ''}</p>
            </div>
            <button className="btn btn-primary" onClick={() => handleDownload(file.path)}>
              <VscCloudDownload /> Download
            </button>
          </div>
          <div className="file-preview-content image-preview">
            <img src={imageUrl} alt={file.name} />
          </div>
        </div>
      );
    } else {
      // Non-editable file preview
      return (
        <div className="file-preview">
          <div className="file-preview-header">
            <div className="file-info">
              <h3>{file.name}</h3>
              <p className="file-path">{file.path}</p>
            </div>
            <button className="btn btn-primary" onClick={() => handleDownload(file.path)}>
              <VscCloudDownload /> Download
            </button>
          </div>
          <div className="file-preview-content">
            <div className="binary-file-notice">
              <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                <polyline points="14 2 14 8 20 8"/>
              </svg>
              <h3>Binary File</h3>
              <p>This file cannot be displayed in the editor</p>
            </div>
          </div>
        </div>
      );
    }
  };

  return (
    <div className="editor-container">
      <div className="editor-tabs">
        {openFiles.length === 0 ? (
          <div className="tab-placeholder">No file open</div>
        ) : (
          <>
            <div className="editor-tabs-scroll">
              {openFiles.map(file => (
                <div
                  key={file.path}
                  className={`editor-tab ${activeTab === file.path ? 'active' : ''} ${modifiedFiles.has(file.path) ? 'modified' : ''}`}
                  onClick={() => setActiveTab(file.path)}
                >
                  <span>{file.name}</span>
                  <button
                    className="close-btn"
                    onClick={(e) => closeFile(file.path, e)}
                  >
                    {modifiedFiles.has(file.path) ? <VscCircleFilled size={12} /> : <VscClose size={14} />}
                  </button>
                </div>
              ))}
            </div>
            {modifiedFiles.size > 0 && (
              <div className="editor-tabs-actions">
                <button
                  className="btn btn-primary save-btn"
                  onClick={saveCurrentFile}
                  title="Save (Ctrl+S)"
                >
                  <VscSave />
                  Save
                </button>
              </div>
            )}
          </>
        )}
      </div>
      <div className="editor-wrapper">
        {activeTab && activeFileData ? (
          // Show preview for images and binary files, editor for editable text files
          (activeFileData.isImage || !activeFileData.isEditable) ? (
            <FilePreview
              file={activeFileData}
              content={fileContents[activeTab] || ''}
              metadata={metadata}
            />
          ) : (
            <MonacoEditor
              key={activeTab}
              height="100%"
              language={activeFileData.language}
              value={fileContents[activeTab] || ''}
              onChange={handleEditorChange}
              onMount={handleEditorDidMount}
              theme={theme === 'dark' ? 'vs-dark' : 'vs-light'}
              options={{
                minimap: { enabled: true },
                fontSize: 14,
                lineNumbers: 'on',
                rulers: [80, 120],
                wordWrap: 'off',
                automaticLayout: true,
                scrollBeyondLastLine: false,
                tabSize: 2,
              }}
            />
          )
        ) : (
          <div className="loading">
            Select a file from the file tree to start editing
          </div>
        )}
      </div>
    </div>
  );
};

export default Editor;
