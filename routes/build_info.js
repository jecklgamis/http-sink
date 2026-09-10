const express = require('express');
const router = express.Router();

const build_info = require('../build-info-data.js');

router.get('/', (req, res, next) => {
    res.end(JSON.stringify(build_info));
});

module.exports = router;
