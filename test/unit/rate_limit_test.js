const chai = require('chai');
const {expect} = chai;
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
});
