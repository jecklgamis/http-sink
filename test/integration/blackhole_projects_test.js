import * as chai from 'chai';
import {default as chaiHttp, request} from "chai-http";
import app from '../../app.js';

const {expect} = chai;

chai.use(chaiHttp);

function uniqueName(prefix) {
    return prefix + '-' + Math.random().toString(36).slice(2, 8);
}

describe('Blackhole project-scoped config', () => {
    it('creates a project and returns a token', async () => {
        const name = uniqueName('proj');
        const res = await request.execute(app).post('/blackhole/projects').send({name});
        expect(res).to.have.status(201);
        expect(res.body.name).to.equal(name);
        expect(res.body.token).to.be.a('string').with.length.greaterThan(10);
    });

    it('rejects creating a project with a name already taken', async () => {
        const name = uniqueName('proj');
        await request.execute(app).post('/blackhole/projects').send({name});
        const res = await request.execute(app).post('/blackhole/projects').send({name});
        expect(res).to.have.status(409);
    });

    it('rejects an invalid project name', async () => {
        const res = await request.execute(app).post('/blackhole/projects').send({name: 'has spaces'});
        expect(res).to.have.status(400);
    });

    it('rejects latency config on bare /blackhole', async () => {
        const res = await request.execute(app)
            .post('/blackhole/config/latency')
            .send({path: '/blackhole', jitterMs: 100});
        expect(res).to.have.status(400);
    });

    it('rejects config for a nonexistent project', async () => {
        const res = await request.execute(app)
            .post('/blackhole/config/latency')
            .send({path: '/blackhole/no-such-project-xyz/foo', jitterMs: 100});
        expect(res).to.have.status(404);
    });

    it('rejects config without a token and with the wrong token, allows it with the right token', async () => {
        const name = uniqueName('proj');
        const createRes = await request.execute(app).post('/blackhole/projects').send({name});
        const token = createRes.body.token;
        const path = `/blackhole/${name}/scenario`;

        const noTokenRes = await request.execute(app)
            .post('/blackhole/config/latency')
            .send({path, jitterMs: 50});
        expect(noTokenRes).to.have.status(403);

        const wrongTokenRes = await request.execute(app)
            .post('/blackhole/config/latency')
            .set('X-Blackhole-Token', 'wrong-token')
            .send({path, jitterMs: 50});
        expect(wrongTokenRes).to.have.status(403);

        const okRes = await request.execute(app)
            .post('/blackhole/config/latency')
            .set('X-Blackhole-Token', token)
            .send({path, jitterMs: 50});
        expect(okRes).to.have.status(201);

        const deleteWrongTokenRes = await request.execute(app)
            .delete('/blackhole/config/latency')
            .query({path})
            .set('X-Blackhole-Token', 'wrong-token');
        expect(deleteWrongTokenRes).to.have.status(403);

        const deleteOkRes = await request.execute(app)
            .delete('/blackhole/config/latency')
            .query({path})
            .set('X-Blackhole-Token', token);
        expect(deleteOkRes).to.have.status(204);
    });

    it('allows configuring the project root path itself (no trailing scenario segment)', async () => {
        const name = uniqueName('proj');
        const createRes = await request.execute(app).post('/blackhole/projects').send({name});
        const token = createRes.body.token;
        const path = `/blackhole/${name}`;

        const res = await request.execute(app)
            .post('/blackhole/config/failure')
            .set('X-Blackhole-Token', token)
            .send({path, rate: 0.5, statusCode: 500});
        expect(res).to.have.status(201);

        const cleanupRes = await request.execute(app)
            .delete('/blackhole/config/failure')
            .query({path})
            .set('X-Blackhole-Token', token);
        expect(cleanupRes).to.have.status(204);
    });
});
