var process = require('process');

module.exports = {
  '/portal/': {
    target: 'http://127.0.0.1:' + (process.env.LODESTAR_PORT || '8083'),
    secure: false,
    logLevel: 'debug',
    changeOrigin: true,
    headers: {
      Connection: 'Keep-Alive',
    },
  },
};
