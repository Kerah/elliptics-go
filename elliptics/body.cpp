//
// Created by Boris S.Bobejko on 22.04.15.
//

#include "body.h"

ell_bulk_blobs *ell_bulk_blobs_new()
{
    try {
        return new ell_bulk_blobs;
    } catch (...) {
        return NULL;
    }
}

void ell_bulk_blobs_free(ell_bulk_blobs *blobs)
{
    delete blobs;
}


int ell_bulk_blobs_insert(ell_bulk_blobs *bulk_blob, ell_io_attr *attr, char *data, uint64_t len)
{
    try {
        bulk_blob->insert(attr, data, len);
        return 0;
    } catch (...) {
        return -ENOMEM;
    }
}