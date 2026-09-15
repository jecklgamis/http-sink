const toIpv4 = ip => ip === '::1' ? '127.0.0.1' : ip.replace(/^::ffff:/, '');

module.exports = {toIpv4};
