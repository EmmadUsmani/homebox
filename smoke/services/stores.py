"""Database client probes. Fixture names are reserved for the smoke VM."""
import os
import sqlite3
import sys
from contextlib import closing

import boto3
import pika
import psycopg
import redis
from botocore.config import Config


def sqlite(phase):
    with closing(sqlite3.connect(os.path.join(os.environ["SMOKE_TMP"], "db"))) as db:
        if phase == "write":
            assert db.execute("PRAGMA journal_mode=WAL").fetchone() == ("wal",)
            with db:
                db.execute("CREATE VIRTUAL TABLE notes USING fts5(body)")
                db.execute("INSERT INTO notes VALUES ('homebox smoke')")
        assert db.execute(
            """SELECT json_extract('{"answer":42}', '$.answer')
               FROM notes WHERE notes MATCH 'homebox'"""
        ).fetchone() == (42,)


def postgres(phase):
    with psycopg.connect("dbname=homebox_smoke connect_timeout=5") as db:
        if phase == "write":
            db.execute("CREATE EXTENSION pgcrypto")
            db.execute("CREATE EXTENSION pg_trgm")
            db.execute("CREATE TABLE notes (body text)")
            db.execute("INSERT INTO notes VALUES ('smoke')")
        assert db.execute(
            "SELECT length(digest(body, 'sha256')), similarity(body, 'smoke') FROM notes"
        ).fetchone() == (32, 1.0)


def mariadb(phase):
    import pymysql

    with pymysql.connect(unix_socket="/run/mysqld/mysqld.sock",
                         user=os.environ["USER"], database="homebox_smoke",
                         autocommit=True, read_timeout=5) as db, db.cursor() as cursor:
        if phase == "write":
            cursor.execute("CREATE TABLE notes (body varchar(20))")
            cursor.execute("INSERT INTO notes VALUES ('smoke')")
        cursor.execute("SELECT body FROM notes")
        assert cursor.fetchone() == ("smoke",)


def redis_store(phase, port):
    with redis.Redis(port=port, socket_timeout=5) as db:
        if phase == "write":
            db.set("note", "smoke")
            db.xadd("stream", {"body": "smoke"})
            with db.pubsub() as inbox:
                inbox.subscribe("test")
                assert inbox.get_message(timeout=3)["type"] == "subscribe"
                assert db.publish("test", "smoke") == 1
                assert inbox.get_message(timeout=3)["data"] == b"smoke"
            db.save()
        assert db.get("note") == b"smoke"
        assert db.xrange("stream")[0][1] == {b"body": b"smoke"}


def memcached(phase):
    from pymemcache.client.base import Client

    with closing(Client(("127.0.0.1", 11211), connect_timeout=5, timeout=5)) as db:
        if phase == "write":
            assert db.set("homebox-smoke", b"smoke", noreply=False)
            assert db.get("homebox-smoke") == b"smoke"
        else:
            assert db.get("homebox-smoke") is None


def rabbitmq(phase):
    options = pika.ConnectionParameters("127.0.0.1", socket_timeout=5, blocked_connection_timeout=5)
    with pika.BlockingConnection(options) as db:
        channel = db.channel()
        queue = "homebox-smoke"
        if phase == "write":
            channel.queue_delete(queue)
            channel.queue_declare(queue, durable=True)
            channel.confirm_delivery()
            channel.basic_publish("", queue, b"smoke", mandatory=True,
                                  properties=pika.BasicProperties(delivery_mode=2))
        else:
            method, _, body = channel.basic_get(queue)
            assert body == b"smoke"
            channel.basic_ack(method.delivery_tag)
            channel.queue_delete(queue)


def s3(phase):
    db = boto3.client("s3", endpoint_url="http://127.0.0.1:19000",
                      aws_access_key_id=os.environ["GARAGE_DEFAULT_ACCESS_KEY"],
                      aws_secret_access_key=os.environ["GARAGE_DEFAULT_SECRET_KEY"],
                      region_name="garage", config=Config(connect_timeout=3, read_timeout=5,
                                                          request_checksum_calculation="when_required",
                                                          response_checksum_validation="when_required"))
    key = {"Bucket": "homebox-smoke", "Key": "note"}
    if phase == "write":
        db.put_object(**key, Body=b"smoke")
    assert db.get_object(**key)["Body"].read() == b"smoke"


def mongo(phase):
    import pymongo

    with pymongo.MongoClient("mongodb://127.0.0.1:19001", serverSelectionTimeoutMS=5000) as db:
        notes = db.homebox_smoke.notes
        if phase == "write":
            notes.insert_one({"body": "smoke"})
        assert notes.find_one({"body": "smoke"}) is not None


probes = {
    "sqlite": sqlite, "postgres": postgres, "mariadb": mariadb,
    "redis": lambda phase: redis_store(phase, 16379),
    "valkey": lambda phase: redis_store(phase, 16380),
    "memcached": memcached, "rabbitmq": rabbitmq, "s3": s3, "mongo": mongo,
}
probes[sys.argv[1]](sys.argv[2])
