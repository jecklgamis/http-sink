const express = require('express');
const router = express.Router();

router.get('/', function (req, res, next) {
    res.render('index', {
        title: 'http-sink',
        message: 'A simple request sink that accepts any method or subpath under /sink, echoes it back, lets you simulate latency and failures (or mock a response entirely), and watch live traffic stats.',
    });
});

module.exports = router;
