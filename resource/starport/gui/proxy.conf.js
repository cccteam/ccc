var process = require('process');

module.exports = {
  '/api/': {
    target: 'http://127.0.0.1:' + (process.env.STARPORT_PORT || '8080'),
    secure: false,
    logLevel: 'debug',
    changeOrigin: true,
    headers: {
      Connection: 'Keep-Alive',
    },
  },
};
