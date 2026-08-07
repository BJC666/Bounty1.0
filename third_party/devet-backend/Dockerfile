FROM python:3.12-slim
WORKDIR /third_party
COPY devet-backend/requirements.txt ./devet-backend/requirements.txt
RUN pip install --no-cache-dir -r devet-backend/requirements.txt
COPY devet-backend/server.py devet-backend/api_routes.py ./devet-backend/
COPY vet-repro ./vet-repro
WORKDIR /third_party/devet-backend
EXPOSE 8765
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8765"]
