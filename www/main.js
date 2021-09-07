let dropArea;

// how many files can we upload in one go
// TODO: maybe make it based on total size
const nFileLimit = 250;

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
    // console.log("preventDefaults");
    e.preventDefault()
    e.stopPropagation()
}

function getDropContainerElement() {
    return document.getElementById("drop-area")
}

const allowedExts = {
    "html": true,
    "htm": true,
    "js": true,
    "css": true,
    "txt": true,
    "md": true,
    "markdown": true,
    "png": true,
    "jpeg": true,
    "jpg": true,
    "gif": true,
    "webp": true,
    "xml": true,
    "avif": true,
}

function allowedFile(file) {
    let name = file.name.toLowerCase();
    //let type = file.type;
    let parts = name.split(".");
    let n = len(parts)
    if (n == 1) {
        // no extension
        return false;
    }
    ext = parts[n - 1];
    return allowedExts[ext];
}

function onId(id, f) {
    const el = document.getElementById(id);
    if (!el) {
        console.log("onId: no element with id:", id);
        return;
    }
    f(el);
}

function showById(id) {
    onId(id, el => el.style.display = "block");
}

function hideById(id) {
    onId(id, el => el.style.display = "none");
}

function showError(msg) {
    onId("upload-error", el => {
        el.style.display = "block";
        el.textContent = msg;
    })
    hideStatus();
}

function hideError() {
    hideById("upload-error");
}

function hideStatus() {
    hideById("upload-status");
}

function showStatus(msg) {
    onId("upload-status", el => {
        el.style.display = "block";
        //el.textContent = msg;
        el.innerHTML = msg;
    })
}

function highlight(e) {
    dropArea.classList.add('highlight')
}

function unhighlight(e) {
    dropArea.classList.remove('highlight')
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

function filterFiles(files) {
    let toSubmit = [];
    let toSkip = [];
    for (const f of files) {
        if (!allowedFile(f)) {
            toSkip.push(f);
            continue;
        }
        toSubmit.push(f);
    }
    return {
        toSubmit: toSubmit,
        toSkip: toSkip,
    }
}

async function uploadFormData(formData) {
    let uploadURL = "/api/upload";
    try {
        const rsp = await fetch(uploadURL, {
            method: 'POST',
            body: formData,
        });
        if (rsp.status != 200) {
            showError(`failed to upload files. /api/upload failed with status code ${rsp.status}`);
            return;
        }
        let uri = await rsp.text();
        showStatus(`Uploaded files. View at <a href="${uri}" target="_blank">${uri}</a>. Will expire in about 2 hrs.`);
    } catch {
        showError("failed to upload files");
    }
}

async function handleFiles(files) {
    let formData = new FormData();
    for (let file of files) {
        formData.append(file.name, file);
    }
    uploadFormData(formData);
}

async function handleDrop(e) {
    preventDefaults(e);

    let dt = e.dataTransfer
    let files = await getAllFileEntries(dt.items);
    let res = filterFiles(files);
    let toSkip = res.toSkip;
    let toSubmit = res.toSubmit;
    // console.log(`toSubmit: ${len(toSubmit)}, toSkip: ${len(toSkip)}`);
    if (len(toSubmit) == 0) {
        showError(`no files to submit out of ${len(files)}`);
        return;
    }
    if (len(toSubmit) > nFileLimit) {
        showError(`Too many files. Limit is ${nFileLimit}, got ${len(toSubmit)}`);
        return;
    }
    hideError();
    let msg = `Uploading ${len(toSubmit)} files`;
    if (len(toSkip) > 0) {
        msg += `, skipping ${len(toSkip)} not supported files`;
    }
    showStatus(msg);
    let formData = new FormData();
    for (let fileEntry of toSubmit) {
        let path = fileEntry.fullPath;
        let file = await new Promise((resolve, reject) => {
            fileEntry.file(resolve, reject);
        })
        formData.append(path, file);
    }
    uploadFormData(formData);
}

function preventDefaultsOnElement(el) {
    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        el.addEventListener(eventName, preventDefaults, false)
    })
}

function onload() {
    // prevent dropping files on body from allowing
    // browser to display the file
    // preventDefaultsOnElement(document.body);
    preventDefaultsOnElement(document.getElementById("body-wrapper"));

    dropArea = getDropContainerElement();
    preventDefaultsOnElement(dropArea);

    ['dragenter', 'dragover'].forEach(eventName => {
        dropArea.addEventListener(eventName, highlight, false);
    })

    // TODO: why a.forEach() doesn't work
    let a = ["dragleave", "drop"];
    a.forEach(eventName => {
        dropArea.addEventListener(eventName, unhighlight, false);
    })

    dropArea.addEventListener('drop', handleDrop, false)
}

async function loadSummary() {
    let rsp = await fetch("/api/summary.json");
    let js = await rsp.json();
    //console.log("loadSummary:", js);
    return js;
}

function onload404Site() {
    console.log("onload404Site()");
}