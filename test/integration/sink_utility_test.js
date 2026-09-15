import * as chai from 'chai';
import {default as chaiHttp, request} from "chai-http";
import app from '../../app.js';

const {expect} = chai;

chai.use(chaiHttp);

describe('Sink utility endpoints', () => {
    it('GET /sink/headers returns request headers', async () => {
        const res = await request.execute(app).get('/sink/headers').set('X-Test', 'abc');
        expect(res).to.have.status(200);
        expect(res.body.headers['x-test']).to.equal('abc');
    });

    it('GET /sink/ip returns the origin', async () => {
        const res = await request.execute(app).get('/sink/ip');
        expect(res).to.have.status(200);
        expect(res.body.origin).to.be.a('string');
    });

    it('GET /sink/user-agent returns the User-Agent header', async () => {
        const res = await request.execute(app).get('/sink/user-agent').set('User-Agent', 'test-agent/1.0');
        expect(res).to.have.status(200);
        expect(res.body['user-agent']).to.equal('test-agent/1.0');
    });

    it('GET /sink/uuid returns a UUID', async () => {
        const res = await request.execute(app).get('/sink/uuid');
        expect(res).to.have.status(200);
        expect(res.body.uuid).to.match(/^[0-9a-f-]{36}$/);
    });

    it('GET /sink/status/:code returns the requested status code', async () => {
        const res = await request.execute(app).get('/sink/status/418');
        expect(res).to.have.status(418);
    });

    it('GET /sink/status/:code rejects an out-of-range code', async () => {
        const res = await request.execute(app).get('/sink/status/999');
        expect(res).to.have.status(400);
    });

    it('GET /sink/delay/:seconds delays the response', async () => {
        const start = Date.now();
        const res = await request.execute(app).get('/sink/delay/0.2');
        expect(res).to.have.status(200);
        expect(Date.now() - start).to.be.at.least(190);
    });

    it('GET /sink/delay/:seconds rejects a negative value', async () => {
        const res = await request.execute(app).get('/sink/delay/-1');
        expect(res).to.have.status(400);
    });

    it('GET /sink/redirect/:n redirects n times then echoes', async () => {
        const res = await request.execute(app).get('/sink/redirect/2').redirects(0);
        expect(res).to.have.status(302);
        expect(res.headers.location).to.equal('/sink/redirect/1');
    });

    it('GET /sink/redirect/0 responds directly without redirecting', async () => {
        const res = await request.execute(app).get('/sink/redirect/0');
        expect(res).to.have.status(200);
        expect(res.body.method).to.equal('GET');
    });

    it('GET /sink/response-headers echoes query params as response headers', async () => {
        const res = await request.execute(app).get('/sink/response-headers?X-Foo=bar');
        expect(res).to.have.status(200);
        expect(res.headers['x-foo']).to.equal('bar');
        expect(res.body['X-Foo']).to.equal('bar');
    });

    it('GET /sink/stream/:n streams n JSON lines', async () => {
        const res = await request.execute(app).get('/sink/stream/3');
        expect(res).to.have.status(200);
        const lines = res.text.trim().split('\n');
        expect(lines).to.have.length(3);
        expect(JSON.parse(lines[0]).id).to.equal(0);
    });

    it('GET /sink/bytes/:n returns n random bytes', async () => {
        const res = await request.execute(app).get('/sink/bytes/16').buffer(true).parse((res, cb) => {
            const chunks = [];
            res.on('data', chunk => chunks.push(chunk));
            res.on('end', () => cb(null, Buffer.concat(chunks)));
        });
        expect(res).to.have.status(200);
        expect(res.body.length).to.equal(16);
    });
});
