function mkel(s) {
    return document.createElement(s);
}

function len(o) {
    if (!o || !o.length) {
        return 0;
    }
    return o.length;
}

function preventDefaults(e) {
    e.preventDefault()
    e.stopPropagation()
}

function getDropContainerElement() {
    return document.getElementById("drop-area")
}

// TODO: very primitive, doesn't work for every word
function plural(n, s) {
    if (n == 1) {
        return s;
    }
    return s + "s";
}

function humanizeSize(i) {
    const kb = 1024;
    const mb = kb * 1024;
    const gb = mb * 1024;
    const tb = gb * 1024;

    function fs(n, d, size) {
        let s = (n / d).toFixed(2);
        s = s.replace(".00", "") + " " + size;
        return s;
    }
    if (i > tb) {
        return fs(i, tb, "TB");
    }
    if (i > gb) {
        return fs(i, gb, "GB")
    }
    if (i > mb) {
        return fs(i, mb, "MB")
    }
    if (i > kb) {
        return fs(i, kb, "kB")
    }
    return `${i} B`;
}

function onId(id, f) {
    const el = document.getElementById(id);
    if (!el) {
        console.log("onId: no element with id:", id);
        return;
    }
    f(el);
}

function showError(msg) {
    let data = Alpine.store('data');
    data.error = msg;
}

function showStatus(msg) {
    let data = Alpine.store('data');
    data.statusHTML = msg;
}

// Wrap readEntries in a promise to make working with readEntries easier
async function readEntriesPromise(directoryReader) {
    try {
        return await new Promise((resolve, reject) => {
            directoryReader.readEntries(resolve, reject);
        });
    } catch (err) {
        console.log(err);
    }
}

async function collectAllDirectoryEntries(directoryReader, queue) {
    let readEntries = await readEntriesPromise(directoryReader);
    while (readEntries.length > 0) {
        queue.push(...readEntries);
        readEntries = await readEntriesPromise(directoryReader);
    }
}

async function getAllFileEntries(dataTransferItemList) {
    let fileEntries = [];
    let queue = [];
    let n = dataTransferItemList.length;
    for (let i = 0; i < n; i++) {
        let item = dataTransferItemList[i];
        let entry = item.webkitGetAsEntry();
        queue.push(entry);
    }
    while (len(queue) > 0) {
        let entry = queue.shift();
        if (entry.isFile) {
            fileEntries.push(entry);
        } else if (entry.isDirectory) {
            let reader = entry.createReader();
            await collectAllDirectoryEntries(reader, queue);
        }
    }
    return fileEntries;
}

function formatDurSince(timeStart) {
    const end = +new Date();
    const durMs = end - timeStart;
    return formatDur(durMs);
}

function formatDur(durMs) {
    if (durMs < 1000) {
        return `${durMs} ms`;
    }
    let secs = (durMs / 1000).toFixed(2);
    secs = secs.replace(".00", "");
    return `${secs} s`;
}

function preventDefaultsOnElement(el) {
    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        el.addEventListener(eventName, preventDefaults, false)
    })
}

async function loadSummary() {
    let rsp = await fetch("/__instantpreviewinternal/api/summary.json");
    let js = await rsp.json();
    //console.log("loadSummary:", js);
    return js;
}