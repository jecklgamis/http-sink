const express = require('express');
const router = express.Router();

router.get('/', function (req, res, next) {
    res.render('index', {
        title: 'http-sink',
        message: 'A simple request sink that accepts any method or subpath under /sink, echoes it back, and lets you simulate latency and failures while watching live traffic stats.',
    });
});

module.exports = router;
