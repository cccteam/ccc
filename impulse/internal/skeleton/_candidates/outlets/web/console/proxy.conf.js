var process = require('process');

module.exports = {
  '/console/api/': {
    target: 'http://127.0.0.1:' + (process.env.PORT || '8093'),
    secure: false,
    logLevel: 'debug',
    changeOrigin: true,
    headers: {
      Connection: 'Keep-Alive',
    },
  },
};
