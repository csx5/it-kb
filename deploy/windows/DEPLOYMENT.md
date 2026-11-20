# Deployment als Windows-Dienst

`it-kb.exe` unterstützt die Windows-Dienststeuerung nativ im Binary (kein NSSM oder Drittanbieter-Wrapper erforderlich).

## 1. Ordner & Konfiguration vorbereiten

Beispielpfad: `C:\Services\it-kb\`

- `it-kb.exe` in den Ordner kopieren
- `config.example.json` als `config.json` kopieren und `data_dir` (z. B. `C:\\Services\\it-kb\\data`), `listen` (z. B. `:8080`) sowie optional `tls_cert`/`tls_key` und `admin_password` anpassen.

## 2. Dienst installieren (PowerShell als Administrator)

```powershell
cd C:\Services\it-kb
.\it-kb.exe service install --config C:\Services\it-kb\config.json
sc.exe start it-kb
```

## 3. Firewall-Freigabe (LAN)

```powershell
New-NetFirewallRule -DisplayName "IT-Wissensdatenbank (it-kb)" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -Profile Domain,Private
```

## 4. Automatische Backups (Aufgabenplanung)

```powershell
C:\Services\it-kb\it-kb.exe backup --config C:\Services\it-kb\config.json --out D:\Backups\it-kb-daily.zip
```

## 5. Dienst entfernen

```powershell
sc.exe stop it-kb
.\it-kb.exe service uninstall
```
