//
// Created by Boris S.Bobejko on 22.04.15.
//

#include "dnet_attr.h"


extern "C" {

ell_io_attr *ell_ell_io_attr_new() {

    try {
        return new ell_io_attr();
    } catch (...) {
        return NULL;
    }
}


void ell_io_attr_set_parent(ell_io_attr *io, char *parent) {
    io->parent = parent;
}

void ell_io_attr_set_size(ell_io_attr *io, uint64_t size) {
    io->size = size;
}

void ell_io_attr_set_offset(ell_io_attr *io, uint64_t offset) {
    io->offset = offset;
}

void ell_io_attr_set_flags(ell_io_attr *io, uint64_t flags) {
    io->flags = flags;
}

void ell_io_attr_set_total_size(ell_io_attr *io, uint64_t total_size) {
    io->total_size = total_size;
}

void ell_io_attr_set_user_flags(ell_io_attr *io, uint64_t user_flags) {
    io->user_flags = user_flags;
}

void ell_io_attr_set_num(ell_io_attr *io, uint64_t num) {
    io->num = num;
}

void ell_io_attr_set_start(ell_io_attr *io, uint64_t start) {
    io->start = start;
}

void ell_io_attr_set_id(ell_io_attr *io, char *id) {
    io->id = id;
}

void ell_io_attr_free(ell_io_attr *attr) {
    delete attr;
}

}