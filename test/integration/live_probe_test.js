import * as chai from 'chai';
import {default as chaiHttp, request} from "chai-http";
import app from '../../app.js';

const {expect} = chai;

chai.use(chaiHttp);

describe('GET /probe/live', () => {
    it('should return status 200 and an object', function (done) {
        request.execute(app)
            .get('/probe/live')
            .end((err, res) => {
                if (err) return done(err);
                expect(res).to.have.status(200);
                expect(res).to.be.an('object');
                done();
            });
    });
});