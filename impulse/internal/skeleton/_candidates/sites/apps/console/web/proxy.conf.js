var process = require('process');

module.exports = {
  '/api/': {
    target: 'http://127.0.0.1:' + (process.env.PORT || '8094'),
    secure: false,
    logLevel: 'debug',
    changeOrigin: true,
    headers: {
      Connection: 'Keep-Alive',
    },
  },
};
