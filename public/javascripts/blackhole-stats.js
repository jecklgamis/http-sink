(function () {
    var POLL_INTERVAL_MS = 1000;

    function renderBreakdown(byPathAndMethod, rpsByPathAndMethod) {
        var rows = [];
        Object.keys(byPathAndMethod).sort().forEach(function (path) {
            var methods = byPathAndMethod[path];
            Object.keys(methods).sort().forEach(function (method) {
                rows.push({
                    path: path,
                    method: method,
                    count: methods[method],
                    rps: rpsByPathAndMethod[path][method],
                });
            });
        });
        rows.sort(function (a, b) { return b.rps - a.rps; });

        var tbody = document.getElementById('breakdown-body');
        tbody.innerHTML = '';
        if (rows.length === 0) {
            var emptyRow = document.createElement('tr');
            var emptyCell = document.createElement('td');
            emptyCell.colSpan = 4;
            emptyCell.textContent = 'no traffic';
            emptyRow.appendChild(emptyCell);
            tbody.appendChild(emptyRow);
            return;
        }
        rows.forEach(function (row) {
            var tr = document.createElement('tr');
            [row.path, row.method, row.rps, row.count].forEach(function (value) {
                var td = document.createElement('td');
                td.textContent = value;
                tr.appendChild(td);
            });
            tbody.appendChild(tr);
        });
    }

    var expandedKeys = {};

    function formatTimeWithMs(timestamp) {
        var d = new Date(timestamp);
        var ms = String(d.getMilliseconds()).padStart(3, '0');
        return d.toLocaleTimeString() + '.' + ms;
    }

    function buildExpandableCell(value, key) {
        var td = document.createElement('td');
        if (value === undefined || value === null) {
            td.textContent = '--';
            return td;
        }
        var compact = JSON.stringify(value);
        var expanded = !!expandedKeys[key];
        td.className = 'body-cell';
        if (expanded) {
            var pre = document.createElement('pre');
            pre.textContent = JSON.stringify(value, null, 2);
            td.appendChild(pre);
        } else {
            td.textContent = compact.length > 60 ? compact.slice(0, 60) + '...' : compact;
        }
        td.addEventListener('click', function () {
            expandedKeys[key] = !expandedKeys[key];
            renderRecent(lastRecentRequests);
        });
        return td;
    }

    var lastRecentRequests = [];

    function renderRecent(recentRequests) {
        lastRecentRequests = recentRequests;
        var tbody = document.getElementById('recent-body');
        tbody.innerHTML = '';
        if (recentRequests.length === 0) {
            var emptyRow = document.createElement('tr');
            var emptyCell = document.createElement('td');
            emptyCell.colSpan = 9;
            emptyCell.textContent = 'no requests yet';
            emptyRow.appendChild(emptyCell);
            tbody.appendChild(emptyRow);
            return;
        }
        recentRequests.forEach(function (r) {
            var key = r.timestamp + ' ' + r.method + ' ' + r.path;
            var tr = document.createElement('tr');
            var time = formatTimeWithMs(r.timestamp);
            var responseTime = r.responseTimeMs === undefined ? '--' : r.responseTimeMs;
            [time, r.method, r.path, r.statusCode || '--', responseTime, r.remoteAddress || '--'].forEach(function (value) {
                var td = document.createElement('td');
                td.textContent = value;
                tr.appendChild(td);
            });

            var userAgentTd = document.createElement('td');
            userAgentTd.className = 'user-agent-cell';
            userAgentTd.textContent = r.userAgent || '--';
            userAgentTd.title = r.userAgent || '';
            tr.appendChild(userAgentTd);

            tr.appendChild(buildExpandableCell(r.headers, key + ':headers'));
            tr.appendChild(buildExpandableCell(r.body, key + ':body'));

            tbody.appendChild(tr);
        });
    }

    var RPS_SPARKLINE_MIN_SCALE = 100;

    var SVG_NS = 'http://www.w3.org/2000/svg';

    function renderRpsGrid(width, height, n) {
        var grid = document.getElementById('rps-sparkline-grid');
        grid.innerHTML = '';

        // horizontal lines at 25/50/75% of height
        [0.25, 0.5, 0.75].forEach(function (t) {
            var y = height * t;
            var line = document.createElementNS(SVG_NS, 'line');
            line.setAttribute('x1', 0);
            line.setAttribute('x2', width);
            line.setAttribute('y1', y);
            line.setAttribute('y2', y);
            line.setAttribute('stroke', '#e0e0e0');
            line.setAttribute('stroke-width', '1');
            line.setAttribute('vector-effect', 'non-scaling-stroke');
            grid.appendChild(line);
        });

        // vertical lines every 5 seconds (buckets)
        for (var i = 5; i < n; i += 5) {
            var x = (i / (n - 1)) * width;
            var vline = document.createElementNS(SVG_NS, 'line');
            vline.setAttribute('x1', x);
            vline.setAttribute('x2', x);
            vline.setAttribute('y1', 0);
            vline.setAttribute('y2', height);
            vline.setAttribute('stroke', '#e0e0e0');
            vline.setAttribute('stroke-width', '1');
            vline.setAttribute('vector-effect', 'non-scaling-stroke');
            grid.appendChild(vline);
        }
    }

    function renderRpsYAxis(max) {
        document.getElementById('rps-yaxis-max').textContent = Math.round(max);
        document.getElementById('rps-yaxis-75').textContent = Math.round(max * 0.75);
        document.getElementById('rps-yaxis-50').textContent = Math.round(max * 0.5);
        document.getElementById('rps-yaxis-25').textContent = Math.round(max * 0.25);
    }

    function renderRpsSparkline(rpsHistory) {
        var width = 300;
        var height = 60;
        var max = Math.max(RPS_SPARKLINE_MIN_SCALE, Math.max.apply(null, rpsHistory));
        var n = rpsHistory.length;
        renderRpsGrid(width, height, n);
        renderRpsYAxis(max);
        var points = rpsHistory.map(function (rps, i) {
            var x = n === 1 ? width : (i / (n - 1)) * width;
            var y = height - (rps / max) * height;
            return x.toFixed(1) + ',' + y.toFixed(1);
        }).join(' ');
        document.getElementById('rps-sparkline-line').setAttribute('points', points);
    }

    var recentFrozen = false;

    var knownPaths = {};

    function updateKnownPaths(byPath) {
        var changed = false;
        Object.keys(byPath).forEach(function (path) {
            if (!knownPaths[path]) {
                knownPaths[path] = true;
                changed = true;
            }
        });
        if (!changed) return;
        var datalist = document.getElementById('failure-path-list');
        datalist.innerHTML = '';
        Object.keys(knownPaths).sort().forEach(function (path) {
            var option = document.createElement('option');
            option.value = path;
            datalist.appendChild(option);
        });
    }

    function update() {
        fetch('/blackhole/stats')
            .then(function (res) { return res.json(); })
            .then(function (data) {
                var w = data.windows['1s'];
                document.getElementById('rps-1s').textContent = w.rps;
                document.getElementById('total-1s').textContent = w.total;
                renderRpsSparkline(data.rpsHistory);
                renderBreakdown(w.byPathAndMethod, w.rpsByPathAndMethod);
                updateKnownPaths(w.byPath);
                if (!recentFrozen) {
                    renderRecent(data.recentRequests);
                }
                document.getElementById('stats-updated').textContent =
                    'last updated ' + new Date(data.now).toLocaleTimeString();
            })
            .catch(function () {
                document.getElementById('stats-updated').textContent = 'stats unavailable';
            });
    }

    document.getElementById('clear-recent').addEventListener('click', function () {
        fetch('/blackhole/recent', {method: 'DELETE'}).then(function () {
            renderRecent([]);
        });
    });

    var freezeButton = document.getElementById('freeze-recent');
    freezeButton.addEventListener('click', function () {
        recentFrozen = !recentFrozen;
        freezeButton.textContent = recentFrozen ? 'Unfreeze' : 'Freeze';
    });

    var PROJECT_STORAGE_KEY = 'blackholeProject';

    function getProject() {
        try {
            var raw = localStorage.getItem(PROJECT_STORAGE_KEY);
            return raw ? JSON.parse(raw) : null;
        } catch (e) {
            return null;
        }
    }

    function setProject(project) {
        try {
            localStorage.setItem(PROJECT_STORAGE_KEY, JSON.stringify(project));
        } catch (e) {
            // ignore (private browsing, storage disabled, etc.)
        }
    }

    function clearProject() {
        try {
            localStorage.removeItem(PROJECT_STORAGE_KEY);
        } catch (e) {
            // ignore
        }
    }

    function getProjectToken() {
        var project = getProject();
        return project ? project.token : null;
    }

    function renderProjectUI() {
        var project = getProject();
        var activeEl = document.getElementById('project-active');
        if (!project) {
            activeEl.hidden = true;
            return;
        }
        document.getElementById('project-active-name').textContent = project.name;
        var prefix = '/blackhole/' + project.name + '/';
        document.getElementById('project-path-prefix').textContent = prefix;
        activeEl.hidden = false;

        var latencyPathInput = document.getElementById('latency-path');
        var failurePathInput = document.getElementById('failure-path');
        if (!latencyPathInput.value) latencyPathInput.value = prefix;
        if (!failurePathInput.value) failurePathInput.value = prefix;
    }

    document.getElementById('project-form').addEventListener('submit', function (e) {
        e.preventDefault();
        var errorEl = document.getElementById('project-form-error');
        errorEl.textContent = '';
        var name = document.getElementById('project-name').value.trim();
        fetch('/blackhole/projects', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({name: name}),
        }).then(function (res) {
            return res.json().then(function (body) {
                return {ok: res.ok, body: body};
            });
        }).then(function (result) {
            if (result.ok) {
                setProject({name: result.body.name, token: result.body.token});
                document.getElementById('project-form').reset();
                renderProjectUI();
                document.getElementById('project-token').value = result.body.token;
                document.getElementById('project-token-reveal').hidden = false;
            } else {
                errorEl.textContent = result.body.error || 'request failed';
            }
        });
    });

    function forgetProjectLocally() {
        clearProject();
        document.getElementById('project-token-reveal').hidden = true;
        document.getElementById('project-token').value = '';
        renderProjectUI();
    }

    document.getElementById('project-forget').addEventListener('click', function () {
        var project = getProject();
        var errorEl = document.getElementById('project-form-error');
        if (!project) {
            forgetProjectLocally();
            return;
        }
        fetch('/blackhole/projects/' + encodeURIComponent(project.name), {
            method: 'DELETE',
            headers: {'X-Blackhole-Token': project.token},
        }).then(function (res) {
            if (res.ok || res.status === 404) {
                if (errorEl) errorEl.textContent = '';
                return;
            }
            return res.json().then(function (body) {
                if (errorEl) {
                    errorEl.textContent = 'Forgot locally, but could not release the name on the server: '
                        + (body.error || res.status);
                }
            });
        }).catch(function () {
            if (errorEl) errorEl.textContent = 'Forgot locally, but could not reach the server to release the name.';
        }).then(forgetProjectLocally);
    });

    document.getElementById('project-token-copy').addEventListener('click', function () {
        var tokenInput = document.getElementById('project-token');
        tokenInput.select();
        if (navigator.clipboard) {
            navigator.clipboard.writeText(tokenInput.value).catch(function () {});
        }
    });

    renderProjectUI();

    function wireConfigSection(opts) {
        function render(configs) {
            var tbody = document.getElementById(opts.tbodyId);
            tbody.innerHTML = '';
            if (configs.length === 0) {
                var emptyRow = document.createElement('tr');
                var emptyCell = document.createElement('td');
                emptyCell.colSpan = opts.fields.length + 1;
                emptyCell.textContent = opts.emptyText;
                emptyRow.appendChild(emptyCell);
                tbody.appendChild(emptyRow);
                return;
            }
            configs.forEach(function (c) {
                var tr = document.createElement('tr');
                opts.fields.forEach(function (f) {
                    var td = document.createElement('td');
                    td.textContent = c[f.key];
                    tr.appendChild(td);
                });
                var actionTd = document.createElement('td');
                var removeButton = document.createElement('button');
                removeButton.textContent = 'Remove';
                removeButton.addEventListener('click', function () {
                    var headers = {};
                    var token = getProjectToken();
                    if (token) headers['X-Blackhole-Token'] = token;
                    fetch(opts.endpoint + '?path=' + encodeURIComponent(c.path), {method: 'DELETE', headers: headers})
                        .then(refresh);
                });
                actionTd.appendChild(removeButton);
                tr.appendChild(actionTd);
                tbody.appendChild(tr);
            });
        }

        function refresh() {
            fetch(opts.endpoint)
                .then(function (res) { return res.json(); })
                .then(render);
        }

        document.getElementById(opts.formId).addEventListener('submit', function (e) {
            e.preventDefault();
            var errorEl = document.getElementById(opts.errorId);
            if (errorEl) errorEl.textContent = '';
            var payload = {};
            opts.fields.forEach(function (f) {
                var raw = document.getElementById(f.id).value;
                payload[f.key] = f.parse ? f.parse(raw) : raw.trim();
            });
            var headers = {'Content-Type': 'application/json'};
            var token = getProjectToken();
            if (token) headers['X-Blackhole-Token'] = token;
            fetch(opts.endpoint, {
                method: 'POST',
                headers: headers,
                body: JSON.stringify(payload),
            }).then(function (res) {
                if (res.ok) {
                    document.getElementById(opts.formId).reset();
                    refresh();
                } else {
                    res.json().then(function (body) {
                        if (errorEl) errorEl.textContent = body.error || 'request failed';
                    }).catch(function () {
                        if (errorEl) errorEl.textContent = 'request failed';
                    });
                }
            });
        });

        refresh();
    }

    wireConfigSection({
        endpoint: '/blackhole/config/latency',
        formId: 'latency-form',
        tbodyId: 'latency-body',
        errorId: 'latency-form-error',
        emptyText: 'no latency configs',
        fields: [
            {id: 'latency-path', key: 'path'},
            {id: 'latency-jitter', key: 'jitterMs', parse: Number},
        ],
    });

    wireConfigSection({
        endpoint: '/blackhole/config/failure',
        formId: 'failure-form',
        tbodyId: 'failure-body',
        errorId: 'failure-form-error',
        emptyText: 'no failure configs',
        fields: [
            {id: 'failure-path', key: 'path'},
            {id: 'failure-rate', key: 'rate', parse: Number},
            {id: 'failure-status', key: 'statusCode', parse: Number},
        ],
    });

    update();
    setInterval(update, POLL_INTERVAL_MS);
})();
