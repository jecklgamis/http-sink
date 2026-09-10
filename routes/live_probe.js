const express = require('express');
const router = express.Router();

router.get('/', (req, res, next) => {
    res.end(JSON.stringify({ok: true, message: "I'm alive!"}));
});

module.exports = router;
