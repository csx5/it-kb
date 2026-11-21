/**
 * Lokaler Sensible-Daten-Warner (100 % im Browser per Regex, ohne externe Aufrufe).
 * Erkennt mögliche Kundendaten oder Geheimnisse vor dem Speichern eines Artikels.
 */

const RULES = [
  {
    category: "Privater Schlüssel / Token",
    regex: /-----BEGIN [A-Z ]*PRIVATE KEY-----|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}/g,
  },
  {
    category: "Klartext-Passwort / Geheimnis",
    regex: /\b(?:passwort|kennwort|password|passwd|pwd|secret|api[_-]?key)\s*[:=]\s*[^\s"'`]{3,}/gi,
  },
  {
    category: "E-Mail-Adresse",
    regex: /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/g,
    ignore: (m) => /@(example\.(com|org|net)|muster\.de|contoso\.com|localhost)$/i.test(m) || /^max\.mustermann@/i.test(m),
  },
  {
    category: "IBAN",
    regex: /\b[A-Z]{2}\d{2}(?:\s?[0-9A-Z]{4}){3,7}\b/g,
  },
  {
    category: "Lizenz- / Produktschlüssel",
    regex: /\b[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}\b/g,
  },
  {
    category: "Interne IPv4-Adresse",
    regex: /\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b/g,
    ignore: (m) => m === "192.168.0.1" || m === "192.168.1.1" || m === "10.0.0.1",
  },
];

function maskSample(text) {
  const s = text.trim();
  if (s.length <= 8) return s;
  return `${s.slice(0, 4)}…${s.slice(-3)}`;
}

export function scanForSensitiveData(fields) {
  const findings = [];
  const seen = new Set();

  for (const { label, value } of fields) {
    if (!value) continue;
    for (const rule of RULES) {
      rule.regex.lastIndex = 0;
      let match;
      while ((match = rule.regex.exec(value)) !== null) {
        const hit = match[0];
        if (rule.ignore && rule.ignore(hit)) continue;
        const key = `${rule.category}|${hit}`;
        if (seen.has(key)) continue;
        seen.add(key);
        findings.push({
          field: label,
          category: rule.category,
          sample: maskSample(hit),
        });
        if (findings.length >= 12) return findings;
      }
    }
  }
  return findings;
}
