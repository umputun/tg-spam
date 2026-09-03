#AntiScam

AntiScam is a repository with two parts:

- Python/FastAPI engine for assessing the risk of phishing messages,
- C# ASP.NET Core WebAPI blog with HTML files and SQLite database.

The new blog is connected to the working folder, and the default SQLite database is created in `data/antiscam-blog.sqlite` (relative to the project directory).

## Requirements

- Python 3.10+
- .NET SDK 8.0+
- beep
- OpenSSL (optional, for generating HTTPS certificates)

## Quick Start: C# WebAPI Blog
```powershell
dotnet restore AntiScamBlog.sln
dotnet run --project src\AntiScam.Blog.Api\AntiScam.Blog.Api.csproj
```
Once the application is launched, it is available at the address displayed by `dotnet run`, usually `http://localhost:5000` or `http://localhost:5080`.

### HTML Frontend

Open in browser:
```text
/
```
The website loads entries from the API and allows you to add a new blog entry.

Before publication, C# WebAPI analyzes the title, abstract, content and author.

The entry will only be saved in `LOW RISK` status; for `MEDIUM RISK` or `HIGH RISK` the API returns `422 Unprocessable Entity` and does not write an entry to SQLite.

### Optional NoSQL database (MongoDB)

SQLite is still the default blog post database.

The optional MongoDB database only stores tickets rejected by risk analysis, so it does not change the operation of the existing SQLite store.

MongoDB is enabled by `NoSql:Enabled`.

The server address can be specified in `NoSql:ConnectionString` or via the `ANTISCAM_MONGO_CONNECTION_STRING` environment variable.

The default database and collection are `antiscam` and `blocked-submissions`.

The current configuration of both storages is returned by `GET /api/storage`.

Rejected reports can be read by `GET /api/incidents?limit=50`.

After enabling MongoDB, the endpoint returns entries from the `blocked-submissions` collection, newest first.

If the database is disabled or unavailable, the response is an empty list.

### Blog endpoints
```text
GET    /api/health
GET    /api/storage
GET    /api/incidents?limit=50
GET    /api/workspace
GET    /api/posts
GET    /api/posts/{slug}
POST   /api/posts
PUT    /api/posts/{id}
DELETE /api/posts/{id}
```
Example of adding an entry:
```powershell
curl -Method POST http://localhost:5000/api/posts `
  -ContentType "application/json" `
  -Body '{"title":"Phishing Alert","summary":"Short description","content":"Post content","author":"AntiScam Team"}'
```
## Quickstart: Python AntiScam API
```powershell
pip install -r requirements-dev.txt
pip install -e .
uvicorn antiscam.api:app --reload
```
The Python API will be available at `http://localhost:8000`.

### Python API endpoints
```text
GET  /
POST /scan
POST /ai/explain
```
Example:
```powershell
curl -Method POST http://localhost:8000/scan `
  -ContentType "application/json" `
  -Body '{"text":"Send BLIK 123456 immediately!"}'
```
The `/ai/explain` endpoint shows practically what AI/NLP facilitates in a project: it recognizes user intent, emotional tone, important terms, proper names, similarity to a fraud pattern and suggests a safe next action.
```powershell
curl -Method POST http://localhost:8000/ai/explain `
  -ContentType "application/json" `
  -Body '{"text":"I\'m scared, Polish Bank wants my BLIK code 123456 urgently"}'
```
### Training the ML model

The project includes a simple Machine Learning model (TF-IDF + Multinomial Naive Bayes) for news classification. To train a model:
```powershell
python train.py
```
The script trains on 16 training samples (8 phishing, 8 safe) and saves the model in `models/model.joblib`.

The model is then used by the Python API to analyze the risk of the message.

### API Documentation (Swagger UI / OpenAPI)

Both applications provide interactive documentation:

**C# Blog WebAPI:**
- Swagger UI: `http://localhost:5000/swagger/ui`
- OpenAPI JSON: `http://localhost:5000/swagger/v1/swagger.json`

**Python AntiScam API:**
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`
- OpenAPI JSON: `http://localhost:8000/openapi.json`

## Run both applications at the same time

To run the project fully (blog + AI engine), open two terminals and run in each:

**Terminal 1 - C# API Blog:**
```powershell
dotnet run --project src\AntiScam.Blog.Api\AntiScam.Blog.Api.csproj
```
**Terminal 2 - Python AntiScam API:**
```powershell
pip install -r requirements-dev.txt
pip install -e .
uvicorn antiscam.api:app --reload
```
The blog will be available at `http://localhost:5000` and the Python API at `http://localhost:8000`.

## Tests

C# tests:
```powershell
dotnet test AntiScamBlog.sln
```
Python tests:
```powershell
pytest
```
The C# project includes unit tests for validation and minions, and integration tests for API, SQLite, and static HTML.

It also includes tests to block the publication of entries that are at risk of phishing or fraud.

Also includes AES-GCM-256 cryptography tests.

## Environment variables

Full list of environment variables used in the project:

| Variable | Description | Default | Example |
|---------|------|----------|----------|
| `ANTISCAM_BLOG_DB` | Path to the blog's SQLite database | `data/antiscam-blog.sqlite` | `C:\temp\antiscam-blog.sqlite` |
| `ANTISCAM_MONGO_CONNECTION_STRING` | MongoDB server address (optional) | - | `mongodb+srv://user:pass@cluster.mongodb.net` |
| `ANTISCAM_HTTPS_CERT_PASSWORD` | HTTPS (OpenSSL) certificate password | - | `strong-local-password` |

**Setting Variables in PowerShell:**
```powershell
$env:ANTISCAM_BLOG_DB="C:\temp\antiscam-blog.sqlite"
```
## Compliance with Syllabi

**The project fully meets the requirements of the syllabi for three courses: Fundamentals of Computer Security, Computer Systems Security, and Information Security.**

The `antiscam` folder contains the implementation of all required learning outcomes, and the materials required for project assessment are located in:

- `SYLLABUS_MAPPING.md` - mapping learning outcomes to code and documentation,
- `docs/ai_syllabus_mapping.md` - syllabus mapping from the `AI_antiscam` folder,
- `docs/project_report.md` - project report,
- `docs/ai_project_report.md` - AI/NLP extension report,
- `docs/security_audit.md` - security overview and checklist,
- `docs/cryptography.md` - description of hashing, encryption and key management,
- `docs/ai_ethics.md` - AI ethics and artificial empathy,
- `docs/demo.md` - demonstration scenario,
- `docs/presentation_outline.md` - presentation outline,
- `docs/labs/` - safety laboratory instructions,
- `docs/ai_labs/` - AI/NLP lab manuals.

## Structure
```text
antiscam/                                  Python AntiScam engine
antiscam/ai.py                             Educational AI/NLP components
tests/                                     Python tests
train.py                                   ML model training (TF-IDF + Naive Bayes)
src/AntiScam.Blog.Api/                    C# ASP.NET Core Blog WebAPI
src/AntiScam.Blog.Api/wwwroot/            HTML, CSS and JS files
tests/AntiScam.Blog.Api.Tests/            Unit and integration tests C#
docs/                                     Report, audit, demo and laboratories
docs/ai_labs/                              Laboratories for AI_antiscam syllabi
SYLLABUS_MAPPING.md                       Project to syllabi mapping
AntiScamBlog.sln                          .NET Solution
README.md                                 Polish documentation
README.en.md                              English documentation
```
## C# Configuration WebAPI Blog

The default settings are in `src/AntiScam.Blog.Api/appsettings.json`:
```json
{
  "Workspace": {
    "RootPath": "C:\\Users\\kondz\\antiscam"
  },
  "Blog": {
    "DatabasePath": "C:\\Users\\kondz\\antiscam\\data\\antiscam-blog.sqlite"
  }
}
```
For testing or local experiments, you can override the database path with an environment variable:
```powershell
$env:ANTISCAM_BLOG_DB="C:\temp\antiscam-blog.sqlite"
```
### HTTPS on the local network (OpenSSL)

The `tools/generate-https-certificate.ps1` script creates a local CA and a PFX certificate with a private IP address in the SAN, analogous to the reference project. After installing OpenSSL, run:
```powershell
$env:ANTISCAM_HTTPS_CERT_PASSWORD = "strong-local-password"
.\tools\generate-https-certificate.ps1 -PrivateIp "192.168.1.22"
```
Then set `Https:Enabled` to `true` and run the application.

Kestrel will listen on `0.0.0.0:5001` and the application will be accessible from the LAN as `https://192.168.1.22:5001`.

To remove the browser warning on network devices, trust the `certs/antiscam-ca.crt` file.

Without a certificate, a simple `dotnet run --project .\src\AntiScam.Blog.Api` listens on all interfaces on port 5000.

For a computer with the address `192.168.1.22`, then use `http://192.168.1.22:5000` from a device on the same network.

If the connection from another device is blocked, allow the .NET application to access incoming traffic in Windows Firewall for Private Networks.

## Updated the English version of the README

To refresh `README.en.md` based on the Polish `README.md`, run:
```powershell
.\tools\sync-readme-en.ps1
```
The script uses `deep-translator` to translate the documentation content and writes the result to `README.en.md`.

## GitHub

The repository is configured with origin:
```text
https://github.com/Kondexor2000/antiscam.git
```
Recommended flow after changes:
```powershell
git status
git add .
git commit -m "Add C# blog WebAPI with SQLite"
git push origin main
```
## Troubleshooting / FAQ

### The port is already in use

**Problem:** "Address already in use" when starting the application.

**Solution - C# API:**
```powershell
# Change the port in appsettings.json or via environment variable:
$env:ASPNETCORE_URLS="http://localhost:5002"
```
**Solution - Python API:**
```powershell
uvicorn antiscam.api:app --reload --port 8001
```
### The SQLite database is locked

**Problem:** "database is locked" during tests or concurrent operations.

**Solution:**
- Make sure only one C# API instance is running
- Close other processes using the database (e.g.

`sqlite3.exe`)
- Delete the `.sqlite-journal` file if it exists

### Python dependencies not installing

**Issue:** Errors during `pip install -r requirements-dev.txt`.

**Solution:**
```powershell
# Update pip and setuptools
python -m pip install --upgrade pip setuptools
# Clear cache
pip cache purge
# Try again
pip install -r requirements-dev.txt
```
### Tests fail

**Issue:** Errors in `pytest` or `dotnet test`.

**Solution:**
```powershell
# Clean cache and rebuild
rm -Force -Recurse bin, obj  # or Remove-Item
rm -Force .pytest_cache
dotnet clean
dotnet build
pytest --tb=short  # Detailed output
```
### OpenSSL certificate issues

**Problem:** HTTPS errors on Windows or certificate not trusted.

**Solution:**
- Run the script as Administrator: `Set-ExecutionPolicy -ExecutionPolicy Unrestricted`
- Make sure OpenSSL is installed: `openssl version`
- Trust CA certificate: `certs/antiscam-ca.crt` (add to Windows Certificate Store)

## License

The project is available under the MIT license.