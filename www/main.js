let dropArea;

let mkel = document.createElement;

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

// Get all the entries (files or sub-directories) in a directory by calling readEntries until it returns empty array
async function readAllDirectoryEntries(directoryReader) {
    let res = [];
    let readEntries = await readEntriesPromise(directoryReader);
    while (readEntries.length > 0) {
        res.push(...readEntries);
        readEntries = await readEntriesPromise(directoryReader);
    }
    return res;
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
            let e = await readAllDirectoryEntries(reader)
            queue.push(...e);
        }
    }
    return fileEntries;
}

async function handleFiles(files) {
    for (const file of files) {
        console.log(file);
    }

    let uploadURL = "/api/upload";
    let formData = new FormData();
    for (const file of files) {
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

async function handleDrop(e) {
    let dt = e.dataTransfer
    console.log("handleDrop: items", dt.items);
    let allFiles = await getAllFileEntries(dt.items);
    let totalSize = 0;
    let toSubmit = [];
    let toSkip = [];
    for (const f of allFiles) {
        if (f.isDirectory) {
            console.log("directory:", f);
            continue;
        }
        if (!allowedFile(f)) {
            toSkip.push(f);
            continue;
        }
        toSubmit.push(f);
        //console.log("size:", f.size());
        //totalSize += f.size;
    }
    console.log(`toSubmit: ${len(toSubmit)}, toSkip: ${len(toSkip)}, totalSize: ${totalSize}`);
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