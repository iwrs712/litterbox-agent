import React, { useState } from 'react';

const AuthModal = ({ onAuthenticated }) => {
  const [token, setToken] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = (e) => {
    e.preventDefault();

    if (!token.trim()) {
      setError('Please enter a token');
      return;
    }

    setError('');
    onAuthenticated(token.trim());
  };

  return (
    <div className="modal-overlay">
      <div className="modal-content">
        <h2>Authentication Required</h2>
        <p>Please enter your access token to continue:</p>
        <form onSubmit={handleSubmit}>
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Enter token"
            autoFocus
          />
          {error && <div className="error-message">{error}</div>}
          <button type="submit" className="btn btn-primary">
            Connect
          </button>
        </form>
      </div>
    </div>
  );
};

export default AuthModal;
