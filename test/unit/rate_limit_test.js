const chai = require('chai');
const {expect} = chai;
const {execFileSync} = require('child_process');
const path = require('path');
const rateLimit = require('../../middleware/rate-limit');

function fakeRes() {
    const res = {};
    res.statusCode = null;
    res.body = null;
    res.status = function (code) {
        res.statusCode = code;
        return res;
    };
    res.json = function (body) {
        res.body = body;
        return res;
    };
    return res;
}

describe('rate limit middleware', () => {
    afterEach(() => {
        rateLimit.reset();
    });

    it('allows up to 1000 requests per second and rejects the 1001st with 429', () => {
        let allowed = 0;
        for (let i = 0; i < 1000; i++) {
            const res = fakeRes();
            rateLimit({}, res, () => {
                allowed++;
            });
            expect(res.statusCode).to.equal(null);
        }
        expect(allowed).to.equal(1000);

        const res = fakeRes();
        let nextCalled = false;
        rateLimit({}, res, () => {
            nextCalled = true;
        });
        expect(nextCalled).to.equal(false);
        expect(res.statusCode).to.equal(429);
        expect(res.body).to.have.property('error');
    });

    it('honors RATE_LIMIT_RPS to override the default limit', () => {
        // LIMIT is read once at module load, so exercise the override in a fresh
        // process rather than mutating process.env after this file's own require()
        const script = `
            process.env.RATE_LIMIT_RPS = '5';
            const rateLimit = require('${path.join(__dirname, '..', '..', 'middleware', 'rate-limit').replace(/\\/g, '\\\\')}');
            const codes = [];
            for (let i = 0; i < 6; i++) {
                const res = {status(c) { this.code = c; return this; }, json() { return this; }};
                rateLimit({}, res, () => { res.code = 200; });
                codes.push(res.code);
            }
            console.log(JSON.stringify(codes));
        `;
        const output = execFileSync(process.execPath, ['-e', script], {encoding: 'utf8'});
        const codes = JSON.parse(output.trim());
        expect(codes).to.deep.equal([200, 200, 200, 200, 200, 429]);
    });
});
