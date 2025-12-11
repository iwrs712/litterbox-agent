// API Service for Litterbox Agent

class ApiService {
  constructor() {
    this.token = '';
    this.baseUrl = '';
    this.onAuthError = null; // Callback for auth errors
  }

  setToken(token) {
    this.token = token;
  }

  getToken() {
    return this.token;
  }

  clearToken() {
    this.token = '';
  }

  setAuthErrorCallback(callback) {
    this.onAuthError = callback;
  }

  async request(endpoint, options = {}) {
    const headers = {
      'Content-Type': 'application/json',
      ...options.headers,
    };

    if (this.token) {
      headers['X-Token'] = this.token;
    }

    const config = {
      ...options,
      headers,
    };

    try {
      const response = await fetch(`${this.baseUrl}${endpoint}`, config);

      // Handle authentication errors
      if (response.status === 401 || response.status === 403) {
        if (this.onAuthError) {
          this.onAuthError();
        }
        throw new Error('Authentication required');
      }

      if (!response.ok) {
        const error = await response.json().catch(() => ({ error: response.statusText }));
        throw new Error(error.error || `HTTP ${response.status}`);
      }

      return await response.json();
    } catch (error) {
      console.error('API Request failed:', error);
      throw error;
    }
  }

  // File Tree
  async getFileTree(path = '') {
    const query = path ? `?path=${encodeURIComponent(path)}` : '';
    return this.request(`/api/tree${query}`, {
      method: 'GET',
    });
  }

  // File Content (GET)
  async getFileContent(path) {
    const query = `?path=${encodeURIComponent(path)}`;
    return this.request(`/api/files${query}`, {
      method: 'GET',
    });
  }

  // Save File (PUT)
  async saveFile(path, content) {
    return this.request('/api/files', {
      method: 'PUT',
      body: JSON.stringify({ path, content }),
    });
  }

  // Create File or Directory (POST)
  async createFile(path, isDir = false) {
    return this.request('/api/files', {
      method: 'POST',
      body: JSON.stringify({ path, is_dir: isDir }),
    });
  }

  // Delete File or Directory (DELETE)
  async deleteFile(path) {
    return this.request('/api/files', {
      method: 'DELETE',
      body: JSON.stringify({ path }),
    });
  }

  // Download File
  async downloadFile(path) {
    const query = `?path=${encodeURIComponent(path)}`;
    const url = `${this.baseUrl}/api/download${query}`;
    const headers = {};
    if (this.token) {
      headers['X-Token'] = this.token;
    }

    try {
      const response = await fetch(url, { headers });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const blob = await response.blob();
      const downloadUrl = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = downloadUrl;
      link.download = path.split('/').pop();
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(downloadUrl);
    } catch (error) {
      console.error('Download failed:', error);
      throw error;
    }
  }

  // Execute Command
  async executeCommand(command, cwd = '') {
    return this.request('/api/exec', {
      method: 'POST',
      body: JSON.stringify({ command, cwd }),
    });
  }

  // Health Check
  async healthCheck() {
    return this.request('/health', {
      method: 'GET',
    });
  }

  // Metrics
  async getMetrics() {
    return this.request('/metrics', {
      method: 'GET',
    });
  }
}

export default new ApiService();
