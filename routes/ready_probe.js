const express = require('express');
const router = express.Router();

router.get('/', (req, res, next) => {
    res.end(JSON.stringify({ok: true, message: "I'm ready!"}));
});

module.exports = router;
