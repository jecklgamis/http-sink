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
        const res = await request.execute(app).post('/sink/projects').send({name});
        expect(res).to.have.status(201);
        expect(res.body.name).to.equal(name);
        expect(res.body.token).to.be.a('string').with.length.greaterThan(10);
    });

    it('rejects creating a project with a name already taken', async () => {
        const name = uniqueName('proj');
        await request.execute(app).post('/sink/projects').send({name});
        const res = await request.execute(app).post('/sink/projects').send({name});
        expect(res).to.have.status(409);
    });

    it('rejects an invalid project name', async () => {
        const res = await request.execute(app).post('/sink/projects').send({name: 'has spaces'});
        expect(res).to.have.status(400);
    });

    it('rejects latency config on bare /sink', async () => {
        const res = await request.execute(app)
            .post('/sink/config/latency')
            .send({path: '/sink', jitterMs: 100});
        expect(res).to.have.status(400);
    });

    it('rejects config for a nonexistent project', async () => {
        const res = await request.execute(app)
            .post('/sink/config/latency')
            .send({path: '/sink/no-such-project-xyz/foo', jitterMs: 100});
        expect(res).to.have.status(404);
    });

    it('rejects config without a token and with the wrong token, allows it with the right token', async () => {
        const name = uniqueName('proj');
        const createRes = await request.execute(app).post('/sink/projects').send({name});
        const token = createRes.body.token;
        const path = `/sink/${name}/scenario`;

        const noTokenRes = await request.execute(app)
            .post('/sink/config/latency')
            .send({path, jitterMs: 50});
        expect(noTokenRes).to.have.status(403);

        const wrongTokenRes = await request.execute(app)
            .post('/sink/config/latency')
            .set('X-Blackhole-Token', 'wrong-token')
            .send({path, jitterMs: 50});
        expect(wrongTokenRes).to.have.status(403);

        const okRes = await request.execute(app)
            .post('/sink/config/latency')
            .set('X-Blackhole-Token', token)
            .send({path, jitterMs: 50});
        expect(okRes).to.have.status(201);

        const deleteWrongTokenRes = await request.execute(app)
            .delete('/sink/config/latency')
            .query({path})
            .set('X-Blackhole-Token', 'wrong-token');
        expect(deleteWrongTokenRes).to.have.status(403);

        const deleteOkRes = await request.execute(app)
            .delete('/sink/config/latency')
            .query({path})
            .set('X-Blackhole-Token', token);
        expect(deleteOkRes).to.have.status(204);
    });

    it('allows configuring the project root path itself (no trailing scenario segment)', async () => {
        const name = uniqueName('proj');
        const createRes = await request.execute(app).post('/sink/projects').send({name});
        const token = createRes.body.token;
        const path = `/sink/${name}`;

        const res = await request.execute(app)
            .post('/sink/config/failure')
            .set('X-Blackhole-Token', token)
            .send({path, rate: 0.5, statusCode: 500});
        expect(res).to.have.status(201);

        const cleanupRes = await request.execute(app)
            .delete('/sink/config/failure')
            .query({path})
            .set('X-Blackhole-Token', token);
        expect(cleanupRes).to.have.status(204);
    });

    it('rejects deleting a project without a token or with the wrong token', async () => {
        const name = uniqueName('proj');
        await request.execute(app).post('/sink/projects').send({name});

        const noTokenRes = await request.execute(app).delete(`/sink/projects/${name}`);
        expect(noTokenRes).to.have.status(403);

        const wrongTokenRes = await request.execute(app)
            .delete(`/sink/projects/${name}`)
            .set('X-Blackhole-Token', 'wrong-token');
        expect(wrongTokenRes).to.have.status(403);
    });

    it('deleting a project with the right token frees the name and cleans up its configs', async () => {
        const name = uniqueName('proj');
        const createRes = await request.execute(app).post('/sink/projects').send({name});
        const token = createRes.body.token;
        const path = `/sink/${name}/foo`;

        await request.execute(app)
            .post('/sink/config/failure')
            .set('X-Blackhole-Token', token)
            .send({path, rate: 0.5, statusCode: 500});

        const deleteRes = await request.execute(app)
            .delete(`/sink/projects/${name}`)
            .set('X-Blackhole-Token', token);
        expect(deleteRes).to.have.status(204);

        const configsRes = await request.execute(app).get('/sink/config/failure');
        expect(configsRes.body.find(c => c.path === path)).to.be.undefined;

        const recreateRes = await request.execute(app).post('/sink/projects').send({name});
        expect(recreateRes).to.have.status(201);
        expect(recreateRes.body.token).to.not.equal(token);
    });
});
