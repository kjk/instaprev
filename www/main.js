function disableDragDrop(evt) {
    evt.preventDefault();
    evt.stopPropagation();

    console.log("disableDragDrop");
}

function onDragEnter(evt) {
    console.log("onDragEnter");

    evt.preventDefault();
    evt.stopPropagation();

    const el = document.getElementById("dropContainer")
    el.classList.add("is-dragover");
}

function onDragOver(evt) {
    console.log("onDragOver");

    evt.preventDefault();
    evt.stopPropagation();
}

function onDrop(evt) {
    console.log("onDrop");
    evt.preventDefault();
    evt.stopPropagation();

    const dt = evt.dataTransfer;
    const n = dt.files.length;
    console.log(`onDrop: ${n} files`);

    for (const file of dt.files) {
        console.log(file);
    }
    // pretty simple -- but not for IE :(
    fileInput.files = evt.dataTransfer.files;

    /*
    // If you want to use some of the dropped files
    const dT = new DataTransfer();
    dT.items.add(evt.dataTransfer.files[0]);
    dT.items.add(evt.dataTransfer.files[3]);
    fileInput.files = dT.files;
    */

    const el = document.getElementById("dropContainer")
    el.classList.remove("is-dragover");
};

function onload() {
    console.log("onload");

    // prevent dropping files on body from allowing
    // browser to display the file
    let dropContainer = document.body;
    dropContainer.ondragover = disableDragDrop;
    dropContainer.ondragenter = disableDragDrop;
    dropContainer.ondrop = disableDragDrop;


    dropContainer = document.getElementById("dropContainer")
    dropContainer.ondragover = onDragOver;
    dropContainer.ondragenter = onDragEnter;
    dropContainer.ondrop = onDrop;
}