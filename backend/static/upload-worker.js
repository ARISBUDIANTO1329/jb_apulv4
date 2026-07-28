// Background Upload Worker - persists across page navigation
// Uses IndexedDB to store pending uploads

const DB_NAME = 'jb_apulv4_uploads';
const DB_VERSION = 1;
const STORE_NAME = 'pending_uploads';

let db = null;

// Initialize IndexedDB
function initDB() {
    return new Promise((resolve, reject) => {
        const request = indexedDB.open(DB_NAME, DB_VERSION);
        request.onupgradeneeded = (e) => {
            const db = e.target.result;
            if (!db.objectStoreNames.contains(STORE_NAME)) {
                db.createObjectStore(STORE_NAME, { keyPath: 'id', autoIncrement: true });
            }
        };
        request.onsuccess = (e) => {
            db = e.target.result;
            resolve(db);
        };
        request.onerror = (e) => reject(e.target.error);
    });
}

// Save upload to IndexedDB
function saveUpload(upload) {
    return new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        const store = tx.objectStore(STORE_NAME);
        const request = store.add(upload);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}

// Get all pending uploads
function getPendingUploads() {
    return new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_NAME, 'readonly');
        const store = tx.objectStore(STORE_NAME);
        const request = store.getAll();
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}

// Remove upload from IndexedDB
function removeUpload(id) {
    return new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        const store = tx.objectStore(STORE_NAME);
        const request = store.delete(id);
        request.onsuccess = () => resolve();
        request.onerror = () => reject(request.error);
    });
}

// Update upload status
function updateUpload(id, updates) {
    return new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        const store = tx.objectStore(STORE_NAME);
        const getReq = store.get(id);
        getReq.onsuccess = () => {
            const upload = getReq.result;
            if (upload) {
                Object.assign(upload, updates);
                store.put(upload);
            }
            resolve();
        };
        getReq.onerror = () => reject(getReq.error);
    });
}

// Process pending uploads
async function processPendingUploads() {
    const pending = await getPendingUploads();
    for (const upload of pending) {
        if (upload.status === 'pending' || upload.status === 'error') {
            await doUpload(upload);
        }
    }
}

// Single upload
async function doUpload(upload) {
    try {
        await updateUpload(upload.id, { status: 'uploading', startedAt: Date.now() });

        const formData = new FormData();
        formData.append('channel_id', upload.channelId);
        formData.append('asset_type', upload.assetType);
        if (upload.metadataCategory) {
            formData.append('metadata_category', upload.metadataCategory);
        }
        formData.append('file', upload.file);

        const response = await fetch('/api/media/upload', {
            method: 'POST',
            body: formData
        });

        if (response.ok) {
            const result = await response.json();
            await updateUpload(upload.id, {
                status: 'completed',
                completedAt: Date.now(),
                result: result
            });
            // Notify UI
            self.postMessage({
                type: 'upload_complete',
                uploadId: upload.id,
                result: result
            });
        } else {
            const error = await response.text();
            await updateUpload(upload.id, {
                status: 'error',
                error: error,
                completedAt: Date.now()
            });
            self.postMessage({
                type: 'upload_error',
                uploadId: upload.id,
                error: error
            });
        }
    } catch (e) {
        await updateUpload(upload.id, {
            status: 'error',
            error: e.message,
            completedAt: Date.now()
        });
        self.postMessage({
            type: 'upload_error',
            uploadId: upload.id,
            error: e.message
        });
    }
}

// Message handler
self.onmessage = async function(e) {
    const { type, data } = e.data;

    switch (type) {
        case 'init':
            await initDB();
            await processPendingUploads();
            self.postMessage({ type: 'ready' });
            break;

        case 'upload':
            const uploadData = {
                channelId: data.channelId,
                assetType: data.assetType,
                metadataCategory: data.metadataCategory,
                file: data.file,
                fileName: data.fileName,
                fileSize: data.fileSize,
                status: 'pending',
                createdAt: Date.now()
            };
            const id = await saveUpload(uploadData);
            self.postMessage({
                type: 'upload_queued',
                uploadId: id,
                fileName: data.fileName
            });
            // Start processing
            await processPendingUploads();
            break;

        case 'retry':
            await updateUpload(data.uploadId, { status: 'pending', error: null });
            await processPendingUploads();
            break;

        case 'cancel':
            await removeUpload(data.uploadId);
            self.postMessage({
                type: 'upload_cancelled',
                uploadId: data.uploadId
            });
            break;

        case 'get_pending':
            const pending = await getPendingUploads();
            self.postMessage({
                type: 'pending_list',
                uploads: pending
            });
            break;
    }
};

// Auto-process on visibility change (when tab becomes visible)
self.addEventListener('visibilitychange', () => {
    if (!document.hidden) {
        processPendingUploads();
    }
});