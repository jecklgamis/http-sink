import * as chai from 'chai';
import {default as chaiHttp, request} from "chai-http";
import app from '../../app.js';

const {expect} = chai;

chai.use(chaiHttp);

describe('GET /metrics', () => {
    it('returns Prometheus-format metrics including a request the test itself just made', async () => {
        await request.execute(app).get('/sink/uuid');

        const res = await request.execute(app).get('/metrics');
        expect(res).to.have.status(200);
        expect(res.headers['content-type']).to.include('text/plain');
        expect(res.text).to.include('# HELP http_requests_total');
        expect(res.text).to.include('# TYPE http_requests_total counter');
        expect(res.text).to.match(/http_requests_total\{method="GET",route="\/sink",status_code="200"\} \d+/);
        expect(res.text).to.include('# HELP http_request_duration_seconds');
    });
});
