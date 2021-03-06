#!/usr/bin/env python

import boto
import boto.s3.connection
from boto.s3 import key

# username. Create bucket directory named so.
access_key = 'noxiouz'
secret_key = 'noxiouz'
host = "localhost"

conn = boto.connect_s3(aws_access_key_id=access_key,
                       aws_secret_access_key=secret_key,
                       host=host,
                       port=9000,
                       is_secure=False,
                       calling_format=boto.s3.connection.OrdinaryCallingFormat(),
                       )


# Create bucket
conn.create_bucket("mybucket")
# Get bucket
bucket = conn.get_bucket("mybucket")
# New key
k = key.Key(bucket)
# Set key name
k.key = "MyKey"
# Set key content and store it
k.set_contents_from_string("BLOB")

# Read key
possible_key = bucket.get_key("MyKey")
print possible_key.get_contents_as_string()


# print bucket listing
for i in bucket.list():
    print i

# list all of buckets owned by me
all_buckets = conn.get_all_buckets()
print all_buckets
