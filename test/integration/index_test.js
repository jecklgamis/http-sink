import * as chai from 'chai';

const {expect} = chai;
import {default as chaiHttp, request} from "chai-http";

chai.use(chaiHttp);
import app from '../../app.js';

describe('GET /', () => {
    it('should return status 200 and an object', function (done) {
        request.execute(app)
            .get('/')
            .end((err, res) => {
                if (err) return done(err);
                expect(res).to.have.status(200);
                expect(res).to.be.an('object');
                done();
            });
    });
});