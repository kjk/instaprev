let dropArea;

const uploadURL = "/api/upload";
const maxFileSize = 1024 * 1024 * 5; // 5 MB
const maxUploadSize = 1024 * 1024 * 10; // 10 MB

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

const blaclistedExt = [
    "exe",
    "mp4",
    "avi",
    "flv",
    "mpg",
    "mpeg",
    "mov",
    "mkv",
    "wmv",
    "dll",
    "so",
];

function allowedFile(name) {
    name = name.toLowerCase();
    let parts = name.split(".");
    let n = len(parts)
    if (n == 1) {
        // no extension
        return true;
    }
    ext = parts[n - 1];
    return !blaclistedExt.includes(ext);
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

// filesWIthPath is []{ file, path }
async function uploadFiles(filesWithPath) {
    // sort by size so that if we skip files due to crossing total size limit,
    // we'll skip the largest files
    function cmpBySize(fwp1, fwp2) {
        const size1 = fwp1.file.size;
        const size2 = fwp2.file.size;
        return size1 - size2;
    }
    filesWithPath.sort(cmpBySize);
    let formData = new FormData();
    let totalSize = 0;
    let nUploading = 0;
    let nSkipping = 0;
    for (const fileWithPath of filesWithPath) {
        let file = fileWithPath.file;
        let name = fileWithPath.path;
        if (!allowedFile(name)) {
            nSkipping++;
            console.log(`Skipping upload of '${name}' ${humanizeSize(file.size)} because file type not supported`);
            continue;
        }
        if (file.size > maxFileSize) {
            console.log(`Skipping upload of '${name}' ${humanizeSize(file.size)}`);
            nSkipping++;
            continue;
        }
        if (totalSize + file.size > maxUploadSize) {
            nSkipping++;
            console.log(`Skipping upload of '${name}' ${humanizeSize(file.size)} because total size would exceed max total size of ${humanizeSize(maxTotalSize)}`);
            continue;
        }
        // console.log(`Uploading '${name}' ('${file.name}') ${humanizeSize(file.size)}`);
        formData.append(name, file);
        totalSize += file.size;
        nUploading++;
    }
    if (nUploading == 0) {
        showError(`No files to upload out of ${len(filesWithPath)}`);
        return;
    }
    hideError();

    let msg = `Uploading ${nUploading} files, ${humanizeSize(totalSize)}`;
    if (nSkipping > 0) {
        msg += `, skipping ${nSkipping} not supported files`;
    }
    showStatus(msg);

    const timeStart = +new Date();
    try {
        const rsp = await fetch(uploadURL, {
            method: 'POST',
            body: formData,
        });
        if (rsp.status != 200) {
            showError(`Failed to upload files. /api/upload failed with status code ${rsp.status}`);
            return;
        }
        let uri = await rsp.text();
        const dur = formatDurSince(timeStart);
        const totalSizeStr = humanizeSize(totalSize);
        showStatus(`<p>Uploaded ${nUploading} ${plural(nUploading, "file")}, ${totalSizeStr} in ${dur}. View at <a href="${uri}" target="_blank">${uri}</a>.</p><p>Will expire in about 2 hrs.</p>`);
    } catch {
        showError("Failed to upload files");
    }
}

async function handleFiles(files) {
    let filesWithPath = [];
    for (const file of files) {
        let fileWithPath = {
            file: file,
            path: file.name,
        }
        filesWithPath.push(fileWithPath);
    }
    uploadFiles(filesWithPath);
}


async function handleDrop(e) {
    preventDefaults(e);

    let dt = e.dataTransfer
    let fileEntries = await getAllFileEntries(dt.items);
    // convert to File objects
    let filesWithPath = [];
    for (let fe of fileEntries) {
        let path = fe.fullPath;
        let file = await new Promise((resolve, reject) => {
            fe.file(resolve, reject);
        })
        let fileWithPath = {
            file: file,
            path: path,
        }
        filesWithPath.push(fileWithPath);
    }
    uploadFiles(filesWithPath);
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