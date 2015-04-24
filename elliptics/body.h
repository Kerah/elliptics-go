//
// Created by Boris S.Bobejko on 22.04.15.
//

#ifndef __ELLIPTICS_GO_BODY_H
#define __ELLIPTICS_GO_BODY_H

#ifdef __cplusplus
#include <iostream>
#include <elliptics/session.hpp>
#include "dnet_attr.h"

extern "C" {
using namespace ioremap;

typedef struct {
    std::vector <std::string> blobs;
    std::vector <ell_io_attr*> attrs;

    void insert(ell_io_attr *attr, char *data, uint64_t len) {
        std::string tmp(data, len);
        blobs.emplace_back(tmp);
        attrs.emplace_back(std::move(attr));
    }
} ell_bulk_blobs;


#else

#include <elliptics/interface.h>
typedef void ell_bulk_blobs;
#endif


ell_bulk_blobs *ell_bulk_blobs_new();

int ell_bulk_blobs_insert(ell_bulk_blobs *bulk_blob, ell_io_attr *attr, char *data, uint64_t len);
void ell_bulk_blobs_free(ell_bulk_blobs *blobs);

#ifdef __cplusplus
}
#endif


#endif //ELLIPTICS_GO_BODY_H
