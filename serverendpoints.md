# API Endpoints

Base URL: `http://<host>:8765`

---

## POST /upload

Lädt eine Audiodatei hoch und startet einen Transkriptionsjob.

**Request**

- Content-Type: `multipart/form-data`
- Body: Feld `file` mit der Audiodatei (z. B. `.wav`, `.mp3`)

**Responses**

| Status | Body | Beschreibung |
|--------|------|--------------|
| `202 Accepted` | `{ "status": "accepted", "job_id": "<uuid>" }` | Job wurde gestartet |
| `400 Bad Request` | `{ "error": "Kein 'file' im Request" }` | Kein `file`-Feld im Request |
| `503 Service Unavailable` | `{ "status": "busy" }` | Server verarbeitet bereits einen Job |

**Beispiel**

```bash
curl -X POST http://localhost:8765/upload \
  -F "file=@aufnahme.wav"
```

---

## GET /status/{job_id}

Gibt den aktuellen Status eines Transkriptionsjobs zurück.

**URL-Parameter**

| Parameter | Typ | Beschreibung |
|-----------|-----|--------------|
| `job_id` | string (UUID) | Die Job-ID aus der `/upload`-Antwort |

**Responses**

| Status | Body | Beschreibung |
|--------|------|--------------|
| `202 Accepted` | `{ "status": "not_completed" }` | Job läuft noch |
| `200 OK` | `{ "status": "completed", "transcript": "<text>" }` | Transkription fertig |
| `404 Not Found` | `{ "error": "Unbekannte job_id" }` | Job-ID nicht gefunden |
| `500 Internal Server Error` | `{ "status": "error", "message": "<fehlermeldung>" }` | Fehler bei der Transkription |

**Beispiel**

```bash
curl http://localhost:8765/status/9ee30be2-2f27-4c7c-996b-26fe5fc51716
```
