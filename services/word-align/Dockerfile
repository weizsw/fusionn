FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN apt-get update && apt-get install -y --no-install-recommends libsndfile1 && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir --index-url https://download.pytorch.org/whl/cpu torch==2.14.1
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
RUN pip install --no-cache-dir --upgrade pip==26.2 setuptools==83.0.0
COPY server.py logfmt.py ./
RUN python -c "import server"
EXPOSE 8767
HEALTHCHECK --interval=15s --timeout=3s CMD ["python", "-c", "from urllib.request import urlopen; urlopen('http://127.0.0.1:8767/health', timeout=2)"]
CMD ["python", "server.py"]
