let dropArea;

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
        //console.log("size:", f.size());
        //totalSize += f.size;
    }
    return {
        toSubmit: toSubmit,
        toSkip: toSkip,
    }
}


async function uploadFiles(files) {
    let uploadURL = "/api/upload";
    let formData = new FormData();
    for (let file of files) {
        let name = "";
        if (file.isFile) {
            name = file.full
            file = file.file();

        }

        formData.append(file.eventName, file);
    }

    try {
        await fetch(uploadURL, {
            method: 'POST',
            body: formData,
        })
        console.log("uploaded files");
    } catch {
        console.log("failed to upload");
    }
}

async function handleFiles(files) {
    for (const file of files) {
        console.log(file);
    }
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
}

async function handleDrop(e) {
    let dt = e.dataTransfer
    console.log("handleDrop: items", dt.items);
    let files = await getAllFileEntries(dt.items);
    let res = filterFiles(files);
    let toSkip = res.toSkip;
    let toSubmit = res.toSubmit;
    if (len(toSubmit) == 0) {
        showError(`no files to submit out of ${len(files)}`);
        return;
    }
    hideById("upload-error");
    showById("list-uploading-wrap");
    onId("list-uploading", ul => {
        for (let file of toSubmit) {
            let li = mkel("li");
            li.textContent = file.name;
            ul.append(li);
        }
    })
    if (len(toSkip) > 0) {
        hideById("list-not-uploading-wrap")
    } else {
        hideById("list-not-uploading-wrap")
    }
    console.log(`toSubmit: ${len(toSubmit)}, toSkip: ${len(toSkip)}`);
}

function highlight(e) {
    dropArea.classList.add('highlight')
}

function unhighlight(e) {
    dropArea.classList.remove('highlight')
}

function preventDefaultsOnElement(el) {
    //console.log("preventDefaultsOnElement", el);
    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        el.addEventListener(eventName, preventDefaults, false)
    })
}

function onload() {
    console.log("onload");

    // prevent dropping files on body from allowing
    // browser to display the file
    // preventDefaultsOnElement(document.body);
    preventDefaultsOnElement(document.getElementById("body-wrapper"));

    //$form = document.getElementsByClassName('my-form')[0];
    //console.log($form);

    dropArea = getDropContainerElement();
    preventDefaultsOnElement(dropArea);

    ['dragenter', 'dragover'].forEach(eventName => {
        dropArea.addEventListener(eventName, highlight, false);
    })

    let a = ["dragleave", "drop"];
    a.forEach(eventName => {
        dropArea.addEventListener(eventName, unhighlight, false);
    })
    dropArea.addEventListener('drop', handleDrop, false)
}