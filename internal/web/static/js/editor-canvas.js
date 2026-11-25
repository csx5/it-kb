/**
 * In-Browser-Screenshot-Editor (HTML5 Canvas).
 * Werkzeuge: Rahmen, Pfeil, Schwärzen, Verpixeln, Zuschneiden, Undo/Redo, Zurücksetzen.
 * Clientseitige Skalierung (max. 1920px) & Komprimierung zu WebP (Fallback PNG) vor Upload.
 */

export async function compressCanvasImage(canvas, maxWidth = 1920, quality = 0.86) {
  let target = canvas;
  if (canvas.width > maxWidth) {
    const ratio = maxWidth / canvas.width;
    target = document.createElement("canvas");
    target.width = maxWidth;
    target.height = Math.round(canvas.height * ratio);
    const ctx = target.getContext("2d");
    ctx.drawImage(canvas, 0, 0, target.width, target.height);
  }

  return new Promise((resolve) => {
    target.toBlob(
      (blob) => {
        if (blob && blob.type === "image/webp") {
          resolve({ blob, ext: "webp" });
          return;
        }
        target.toBlob((pngBlob) => resolve({ blob: pngBlob, ext: "png" }), "image/png");
      },
      "image/webp",
      quality
    );
  });
}

export function createImageEditorController() {
  const dialog = document.getElementById("image-editor-dialog");
  const canvas = document.getElementById("img-ed-canvas");
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  const btnUndo = document.getElementById("img-ed-undo");
  const btnRedo = document.getElementById("img-ed-redo");
  const btnReset = document.getElementById("img-ed-reset");
  const btnCancel = document.getElementById("img-ed-cancel");
  const btnConfirm = document.getElementById("img-ed-confirm");
  const inputAlt = document.getElementById("img-ed-alt");
  const metaInfo = document.getElementById("img-ed-info");
  const toolBtns = [...document.querySelectorAll("[data-img-tool]")];
  const colorBtns = [...document.querySelectorAll("[data-img-color]")];
  const btnZoomOut = document.getElementById("img-ed-zoom-out");
  const btnZoomIn = document.getElementById("img-ed-zoom-in");
  const btnZoomFit = document.getElementById("img-ed-zoom-fit");
  const zoomLabel = document.getElementById("img-ed-zoom-label");

  const ZOOM_STEPS = [0.5, 0.75, 1.0, 1.5, 2.0, 3.0];

  let tool = "redact";
  let color = "#ef4444";
  let zoomMode = "fit";
  let drawing = false;
  let startX = 0;
  let startY = 0;
  let baseSnapshot = null;
  let originalSnapshot = null;
  let history = [];
  let historyIdx = -1;
  let activeResolve = null;

  function applyZoom(mode) {
    zoomMode = mode;
    if (mode === "fit" || canvas.width <= 0) {
      canvas.style.width = "";
      canvas.classList.remove("zoomed");
      if (zoomLabel) zoomLabel.textContent = "Anpassen";
      return;
    }
    canvas.classList.add("zoomed");
    canvas.style.width = `${Math.round(canvas.width * mode)}px`;
    if (zoomLabel) zoomLabel.textContent = `${Math.round(mode * 100)} %`;
  }

  function stepZoom(delta) {
    let idx = zoomMode === "fit" ? ZOOM_STEPS.indexOf(1.0) : ZOOM_STEPS.indexOf(zoomMode);
    if (idx < 0) idx = 2;
    const nextIdx = Math.max(0, Math.min(ZOOM_STEPS.length - 1, idx + delta));
    applyZoom(ZOOM_STEPS[nextIdx]);
  }

  function setTool(next) {
    tool = next;
    toolBtns.forEach((b) => b.classList.toggle("active", b.dataset.imgTool === next));
  }

  function setColor(next) {
    color = next;
    colorBtns.forEach((b) => b.classList.toggle("active", b.dataset.imgColor === next));
  }

  function snapshotCanvas() {
    const snap = document.createElement("canvas");
    snap.width = canvas.width;
    snap.height = canvas.height;
    snap.getContext("2d").drawImage(canvas, 0, 0);
    return snap;
  }

  function restoreSnapshot(snap) {
    canvas.width = snap.width;
    canvas.height = snap.height;
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(snap, 0, 0);
    applyZoom(zoomMode);
    updateInfo();
  }

  function pushHistory() {
    history = history.slice(0, historyIdx + 1);
    history.push(snapshotCanvas());
    if (history.length > 30) {
      history.shift();
    }
    historyIdx = history.length - 1;
    updateButtons();
  }

  function updateButtons() {
    btnUndo.disabled = historyIdx <= 0;
    btnRedo.disabled = historyIdx >= history.length - 1;
    btnConfirm.textContent = "Einfügen (Enter)";
  }

  function updateInfo() {
    metaInfo.textContent = `${canvas.width} × ${canvas.height} px`;
  }

  function pointerPos(e) {
    const rect = canvas.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0 || canvas.width <= 0 || canvas.height <= 0) {
      return { x: 0, y: 0 };
    }
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;
    return {
      x: Math.max(0, Math.min(canvas.width, (e.clientX - rect.left) * scaleX)),
      y: Math.max(0, Math.min(canvas.height, (e.clientY - rect.top) * scaleY)),
    };
  }

  function drawOverlay(x1, y1, x2, y2, isFinal) {
    const rx = Math.min(x1, x2);
    const ry = Math.min(y1, y2);
    const rw = Math.abs(x2 - x1);
    const rh = Math.abs(y2 - y1);
    const lineW = Math.max(3, Math.round(Math.max(canvas.width, canvas.height) / 280));

    if (tool === "rect") {
      ctx.save();
      ctx.strokeStyle = color;
      ctx.lineWidth = lineW;
      ctx.strokeRect(rx, ry, rw, rh);
      ctx.restore();
    } else if (tool === "arrow") {
      drawArrow(ctx, x1, y1, x2, y2, color, lineW);
    } else if (tool === "redact") {
      ctx.save();
      ctx.fillStyle = "#000000";
      ctx.fillRect(rx, ry, rw, rh);
      ctx.restore();
    } else if (tool === "pixelate") {
      if (rw >= 4 && rh >= 4) {
        pixelateRegion(ctx, Math.floor(rx), Math.floor(ry), Math.ceil(rw), Math.ceil(rh));
      }
      if (!isFinal) {
        ctx.save();
        ctx.strokeStyle = "#ffffff";
        ctx.setLineDash([6, 4]);
        ctx.lineWidth = 2;
        ctx.strokeRect(rx, ry, rw, rh);
        ctx.restore();
      }
    } else if (tool === "crop") {
      if (isFinal && rw >= 16 && rh >= 16) {
        const cropped = document.createElement("canvas");
        cropped.width = Math.round(rw);
        cropped.height = Math.round(rh);
        cropped.getContext("2d").drawImage(
          baseSnapshot,
          Math.round(rx), Math.round(ry), Math.round(rw), Math.round(rh),
          0, 0, cropped.width, cropped.height
        );
        restoreSnapshot(cropped);
      } else if (!isFinal) {
        ctx.save();
        ctx.fillStyle = "rgba(0, 0, 0, 0.45)";
        ctx.fillRect(0, 0, canvas.width, ry);
        ctx.fillRect(0, ry + rh, canvas.width, canvas.height - (ry + rh));
        ctx.fillRect(0, ry, rx, rh);
        ctx.fillRect(rx + rw, ry, canvas.width - (rx + rw), rh);
        ctx.strokeStyle = "#60a5fa";
        ctx.lineWidth = 2;
        ctx.strokeRect(rx, ry, rw, rh);
        ctx.restore();
      }
    }
  }

  function drawArrow(c, x1, y1, x2, y2, strokeColor, lineW) {
    const headLen = Math.max(14, lineW * 4.2);
    const angle = Math.atan2(y2 - y1, x2 - x1);
    c.save();
    c.strokeStyle = strokeColor;
    c.fillStyle = strokeColor;
    c.lineWidth = lineW;
    c.lineCap = "round";
    c.beginPath();
    c.moveTo(x1, y1);
    c.lineTo(x2, y2);
    c.stroke();

    c.beginPath();
    c.moveTo(x2, y2);
    c.lineTo(x2 - headLen * Math.cos(angle - Math.PI / 6), y2 - headLen * Math.sin(angle - Math.PI / 6));
    c.lineTo(x2 - headLen * Math.cos(angle + Math.PI / 6), y2 - headLen * Math.sin(angle + Math.PI / 6));
    c.closePath();
    c.fill();
    c.restore();
  }

  function pixelateRegion(c, x, y, w, h) {
    const bx = Math.max(0, x);
    const by = Math.max(0, y);
    const bw = Math.min(canvas.width - bx, w);
    const bh = Math.min(canvas.height - by, h);
    if (bw <= 0 || bh <= 0) return;

    const blockSize = Math.max(10, Math.round(Math.max(canvas.width, canvas.height) / 95));
    const imgData = c.getImageData(bx, by, bw, bh);
    const data = imgData.data;

    for (let py = 0; py < bh; py += blockSize) {
      for (let px = 0; px < bw; px += blockSize) {
        let r = 0, g = 0, b = 0, count = 0;
        const maxY = Math.min(py + blockSize, bh);
        const maxX = Math.min(px + blockSize, bw);
        for (let yy = py; yy < maxY; yy++) {
          for (let xx = px; xx < maxX; xx++) {
            const idx = (yy * bw + xx) * 4;
            r += data[idx];
            g += data[idx + 1];
            b += data[idx + 2];
            count++;
          }
        }
        r = Math.round(r / count);
        g = Math.round(g / count);
        b = Math.round(b / count);
        for (let yy = py; yy < maxY; yy++) {
          for (let xx = px; xx < maxX; xx++) {
            const idx = (yy * bw + xx) * 4;
            data[idx] = r;
            data[idx + 1] = g;
            data[idx + 2] = b;
            data[idx + 3] = 255;
          }
        }
      }
    }
    c.putImageData(imgData, bx, by);
  }

  function drawTextLabel(c, x, y, text, fillColor) {
    const fontSize = Math.max(16, Math.round(Math.max(canvas.width, canvas.height) / 42));
    c.save();
    c.font = `700 ${fontSize}px system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`;
    c.textBaseline = "top";
    c.lineJoin = "round";
    c.lineWidth = Math.max(3, Math.round(fontSize / 5));
    c.strokeStyle = "#000000";
    c.fillStyle = fillColor;
    c.strokeText(text, x, y);
    c.fillText(text, x, y);
    c.restore();
  }

  canvas.addEventListener("pointerdown", (e) => {
    if (e.button !== 0) return;
    const pos = pointerPos(e);
    if (tool === "text") {
      const label = prompt("Text für Screenshot:");
      if (label && label.trim()) {
        drawTextLabel(ctx, pos.x, pos.y, label.trim(), color);
        pushHistory();
      }
      return;
    }
    canvas.setPointerCapture(e.pointerId);
    startX = pos.x;
    startY = pos.y;
    baseSnapshot = snapshotCanvas();
    drawing = true;
  });

  canvas.addEventListener("pointermove", (e) => {
    if (!drawing || !baseSnapshot) return;
    const pos = pointerPos(e);
    restoreSnapshot(baseSnapshot);
    drawOverlay(startX, startY, pos.x, pos.y, false);
  });

  canvas.addEventListener("pointerup", (e) => {
    if (!drawing || !baseSnapshot) return;
    drawing = false;
    const pos = pointerPos(e);
    restoreSnapshot(baseSnapshot);
    if (Math.hypot(pos.x - startX, pos.y - startY) >= 4) {
      drawOverlay(startX, startY, pos.x, pos.y, true);
      pushHistory();
    }
    baseSnapshot = null;
  });

  toolBtns.forEach((b) => b.addEventListener("click", () => setTool(b.dataset.imgTool)));
  colorBtns.forEach((b) => b.addEventListener("click", () => setColor(b.dataset.imgColor)));
  if (btnZoomOut) btnZoomOut.addEventListener("click", () => stepZoom(-1));
  if (btnZoomIn) btnZoomIn.addEventListener("click", () => stepZoom(1));
  if (btnZoomFit) btnZoomFit.addEventListener("click", () => applyZoom("fit"));

  btnUndo.addEventListener("click", () => {
    if (historyIdx > 0) {
      historyIdx--;
      restoreSnapshot(history[historyIdx]);
      updateButtons();
    }
  });

  btnRedo.addEventListener("click", () => {
    if (historyIdx < history.length - 1) {
      historyIdx++;
      restoreSnapshot(history[historyIdx]);
      updateButtons();
    }
  });

  btnReset.addEventListener("click", () => {
    if (originalSnapshot) {
      restoreSnapshot(originalSnapshot);
      originalSnapshot = snapshotCanvas();
      history = [snapshotCanvas()];
      historyIdx = 0;
      updateButtons();
    }
  });

  async function confirmAndClose() {
    if (!activeResolve) return;
    btnConfirm.disabled = true;
    try {
      const { blob, ext } = await compressCanvasImage(canvas);
      const baseName = (inputAlt.value || "screenshot").trim().replace(/[^\w\-äöüÄÖÜß]+/g, "_") || "screenshot";
      const resolve = activeResolve;
      activeResolve = null;
      dialog.close();
      resolve({ blob, filename: `${baseName}.${ext}` });
    } finally {
      btnConfirm.disabled = false;
    }
  }

  function cancelAndClose() {
    if (!activeResolve) return;
    const resolve = activeResolve;
    activeResolve = null;
    dialog.close();
    resolve(null);
  }

  btnConfirm.addEventListener("click", confirmAndClose);
  btnCancel.addEventListener("click", cancelAndClose);
  dialog.addEventListener("cancel", (e) => {
    e.preventDefault();
    cancelAndClose();
  });

  dialog.addEventListener("keydown", (e) => {
    if (!dialog.open) return;
    if (e.key === "Enter" && document.activeElement !== inputAlt && document.activeElement !== btnCancel) {
      if (document.activeElement?.tagName === "BUTTON" && document.activeElement !== btnConfirm) return;
      e.preventDefault();
      confirmAndClose();
    } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
      e.preventDefault();
      if (e.shiftKey) btnRedo.click();
      else btnUndo.click();
    } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "y") {
      e.preventDefault();
      btnRedo.click();
    }
  });

  /**
   * Öffnet das Bildeditor-Modal für einen Bild-Blob und liefert { blob, filename } oder null zurück.
   */
  async function open(fileOrBlob, suggestedName = "screenshot") {
    const url = URL.createObjectURL(fileOrBlob);
    try {
      const img = await new Promise((resolve, reject) => {
        const i = new Image();
        i.onload = () => resolve(i);
        i.onerror = reject;
        i.src = url;
      });

      canvas.width = img.naturalWidth;
      canvas.height = img.naturalHeight;
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.drawImage(img, 0, 0);

      originalSnapshot = snapshotCanvas();
      history = [snapshotCanvas()];
      historyIdx = 0;
      setTool("redact");
      setColor("#ef4444");
      applyZoom("fit");
      inputAlt.value = suggestedName.replace(/\.[^.]+$/, "");
      updateInfo();
      updateButtons();

      dialog.showModal();
      btnConfirm.focus();

      return await new Promise((resolve) => {
        activeResolve = resolve;
      });
    } finally {
      URL.revokeObjectURL(url);
    }
  }

  return { open };
}
