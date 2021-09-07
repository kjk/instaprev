let dropArea;

function preventDefaults(e) {
    // console.log("preventDefaults");
    e.preventDefault()
    e.stopPropagation()
}

function getDropContainerElement() {
    return document.getElementById("drop-area")
}

function allowedFile(file) {
    let name = file.name;
    let type = file.type;
}

async function getAllFileEntries(dataTransferItemList) {
    let fileEntries = [];
    // Use BFS to traverse entire directory/file structure
    let queue = [];
    // Unfortunately dataTransferItemList is not iterable i.e. no forEach
    let n = dataTransferItemList.length;
    for (let i = 0; i < n; i++) {
        let item = dataTransferItemList[i];
        let entry = item.webkitGetAsEntry();
        console.log(entry);
        queue.push(entry);
    }
    while (queue.length > 0) {
        let entry = queue.shift();
        if (entry.isFile) {
            fileEntries.push(entry);
        } else if (entry.isDirectory) {
            let reader = entry.createReader();
            let entries = await readAllDirectoryEntries(reader)
            queue.push(...entries);
        }
    }
    return fileEntries;
}

// Get all the entries (files or sub-directories) in a directory by calling readEntries until it returns empty array
async function readAllDirectoryEntries(directoryReader) {
    let entries = [];
    let readEntries = await readEntriesPromise(directoryReader);
    while (readEntries.length > 0) {
        entries.push(...readEntries);
        readEntries = await readEntriesPromise(directoryReader);
    }
    return entries;
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
    let files = await getAllFileEntries(dt.items);
    console.log("handleDrop: files:", files);
}

function highlight(e) {
    dropArea.classList.add('highlight')
}

function unhighlight(e) {
    dropArea.classList.remove('highlight')
}

function preventDefaultsOnElement(el) {
    console.log("preventDefaultsOnElement", el);
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