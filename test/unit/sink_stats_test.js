const chai = require('chai');
const {expect} = chai;
const stats = require('../../middleware/sink/stats');

describe('sink stats unique IPs', () => {
    it('groups requests by remote address, sorted by count descending', () => {
        stats.record('GET', '/sink/a', '10.0.0.1', undefined, 'ua', {}, 1, 200);
        stats.record('GET', '/sink/b', '10.0.0.1', undefined, 'ua', {}, 1, 200);
        stats.record('GET', '/sink/a', '10.0.0.2', undefined, 'ua', {}, 1, 200);

        const {uniqueIps, uniqueIpWindowSeconds} = stats.computeStats();

        expect(uniqueIpWindowSeconds).to.be.a('number');
        const byIp = Object.fromEntries(uniqueIps.map(r => [r.ip, r.count]));
        expect(byIp['10.0.0.1']).to.equal(2);
        expect(byIp['10.0.0.2']).to.equal(1);
        expect(uniqueIps[0].ip).to.equal('10.0.0.1');
        uniqueIps.forEach(r => expect(r.lastSeen).to.be.a('number'));
    });
});
