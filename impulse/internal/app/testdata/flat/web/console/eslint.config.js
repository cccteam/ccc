// @ts-check
const eslint = require('@eslint/js');

module.exports = [
  {
    // Generator output is the source of truth; lint has nothing to say about it.
    ignores: ['**/zz_gen_*.ts'],
  },
  eslint.configs.recommended,
];
