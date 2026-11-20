/**
 * Modul-Gerüst für den In-Browser-Screenshot-Editor (Phase 3).
 *
 * Vorgesehener Ablauf bei Strg+V im Artikel-Editor:
 * 1. Clipboard-Blob aus paste-Event abfangen (image/*).
 * 2. Modal mit HTML5-Canvas öffnen:
 *    - Werkzeuge: Rahmen, Pfeil, Schwärzen (Deckbalken), Verpixeln (Mosaik), Zuschneiden, Undo/Redo.
 *    - Schnellpfad: Enter oder Button "Direkt einfügen".
 * 3. Clientseitige Skalierung (max. 1920px Breite) & Komprimierung zu WebP (Fallback PNG),
 *    damit ungeschwärzte Original-Screenshots den Browser nie verlassen.
 * 4. Upload an POST /api/media -> Markdown-Bildlink direkt an Cursor-Position einfügen.
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
        if (blob) {
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
