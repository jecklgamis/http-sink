import * as chai from 'chai';
import {default as chaiHttp, request} from "chai-http";
import app from '../../app.js';

const {expect} = chai;

chai.use(chaiHttp);

function uniqueName(prefix) {
    return prefix + '-' + Math.random().toString(36).slice(2, 8);
}

async function createProject() {
    const name = uniqueName('proj');
    const res = await request.execute(app).post('/sink/projects').send({name});
    return {name, token: res.body.token};
}

describe('Sink response templates', () => {
    it('rejects setting a template without a valid project token', async () => {
        const {name} = await createProject();
        const res = await request.execute(app)
            .post('/sink/config/response')
            .send({path: `/sink/${name}/mock`, statusCode: 200, body: {ok: true}});
        expect(res).to.have.status(403);
    });

    it('rejects an out-of-range statusCode', async () => {
        const {name, token} = await createProject();
        const res = await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path: `/sink/${name}/mock`, statusCode: 999, body: {}});
        expect(res).to.have.status(400);
    });

    it('rejects non-object headers', async () => {
        const {name, token} = await createProject();
        const res = await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path: `/sink/${name}/mock`, statusCode: 200, headers: 'not-an-object', body: {}});
        expect(res).to.have.status(400);
    });

    it('serves the configured status/headers/body on the exact path', async () => {
        const {name, token} = await createProject();
        const path = `/sink/${name}/users`;

        const setRes = await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path, statusCode: 201, headers: {'X-Mock': 'yes'}, body: {id: 42, name: 'Ada'}});
        expect(setRes).to.have.status(201);

        const hitRes = await request.execute(app).get(path);
        expect(hitRes).to.have.status(201);
        expect(hitRes.headers['x-mock']).to.equal('yes');
        expect(hitRes.body).to.deep.equal({id: 42, name: 'Ada'});
    });

    it('leaves other paths under the same project echoing normally', async () => {
        const {name, token} = await createProject();
        await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path: `/sink/${name}/mocked`, statusCode: 200, body: {mocked: true}});

        const res = await request.execute(app).get(`/sink/${name}/not-mocked`);
        expect(res).to.have.status(200);
        expect(res.body.method).to.equal('GET');
        expect(res.body.mocked).to.be.undefined;
    });

    it('failure injection still takes precedence over a configured template', async () => {
        const {name, token} = await createProject();
        const path = `/sink/${name}/flaky`;

        await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path, statusCode: 200, body: {ok: true}});
        await request.execute(app)
            .post('/sink/config/failure')
            .set('X-Sink-Token', token)
            .send({path, rate: 1, statusCode: 503});

        const res = await request.execute(app).get(path);
        expect(res).to.have.status(503);
    });

    it('deleting the template restores the normal echo', async () => {
        const {name, token} = await createProject();
        const path = `/sink/${name}/toggled`;

        await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path, statusCode: 200, body: {mocked: true}});

        const deleteRes = await request.execute(app)
            .delete('/sink/config/response')
            .query({path})
            .set('X-Sink-Token', token);
        expect(deleteRes).to.have.status(204);

        const res = await request.execute(app).get(path);
        expect(res.body.mocked).to.be.undefined;
        expect(res.body.method).to.equal('GET');
    });

    it('a method-scoped template only matches that method', async () => {
        const {name, token} = await createProject();
        const path = `/sink/${name}/thing`;

        await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path, method: 'GET', statusCode: 200, body: {via: 'get-template'}});

        const getRes = await request.execute(app).get(path);
        expect(getRes.body).to.deep.equal({via: 'get-template'});

        const postRes = await request.execute(app).post(path);
        expect(postRes.body.via).to.be.undefined;
        expect(postRes.body.method).to.equal('POST');
    });

    it('a method-less template matches any method', async () => {
        const {name, token} = await createProject();
        const path = `/sink/${name}/any`;

        await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path, statusCode: 202, body: {via: 'any-template'}});

        const getRes = await request.execute(app).get(path);
        expect(getRes.body).to.deep.equal({via: 'any-template'});

        const postRes = await request.execute(app).post(path);
        expect(postRes.body).to.deep.equal({via: 'any-template'});
    });

    it('rejects an invalid method', async () => {
        const {name, token} = await createProject();
        const res = await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path: `/sink/${name}/thing`, method: 'TRACE', statusCode: 200, body: {}});
        expect(res).to.have.status(400);
    });

    it('deleting a method-scoped template requires the matching method', async () => {
        const {name, token} = await createProject();
        const path = `/sink/${name}/thing`;

        await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path, method: 'GET', statusCode: 200, body: {}});

        const deleteRes = await request.execute(app)
            .delete('/sink/config/response')
            .query({path, method: 'GET'})
            .set('X-Sink-Token', token);
        expect(deleteRes).to.have.status(204);

        const res = await request.execute(app).get(path);
        expect(res.body.method).to.equal('GET');
    });

    it('deleting the project cleans up its response templates', async () => {
        const {name, token} = await createProject();
        const path = `/sink/${name}/mock`;

        await request.execute(app)
            .post('/sink/config/response')
            .set('X-Sink-Token', token)
            .send({path, statusCode: 200, body: {mocked: true}});

        await request.execute(app)
            .delete(`/sink/projects/${name}`)
            .set('X-Sink-Token', token);

        const listRes = await request.execute(app).get('/sink/config/response');
        expect(listRes.body.find(c => c.path === path)).to.be.undefined;
    });
});
