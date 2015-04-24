//
// Created by Boris S.Bobejko on 22.04.15.
//

#ifndef __ELLIPTICS__DNET_ATTR_H
#define __ELLIPTICS__DNET_ATTR_H

#ifdef __cplusplus
#include <iostream>
#include <elliptics/session.hpp>

extern "C" {
#else
#include <elliptics/interface.h>
#endif

typedef struct {
    char *id;
    char *parent;

    uint64_t start, num;
    uint64_t user_flags;
    uint64_t total_size;
    uint64_t reserved1;
    uint32_t reserved2;
    uint32_t flags;
    uint64_t offset;
    uint64_t size;

} ell_io_attr;


ell_io_attr *ell_ell_io_attr_new();

void ell_io_attr_free(ell_io_attr  *attrs);

void ell_io_attr_set_size(ell_io_attr  *io, uint64_t size);

void ell_io_attr_set_offset(ell_io_attr  *io, uint64_t offset);

void ell_io_attr_set_flags(ell_io_attr  *io, uint64_t flags);

void ell_io_attr_set_total_size(ell_io_attr  *io, uint64_t total_size);

void ell_io_attr_set_user_flags(ell_io_attr  *io, uint64_t user_flags);

void ell_io_attr_set_num(ell_io_attr  *io, uint64_t num);

void ell_io_attr_set_start(ell_io_attr  *io, uint64_t start);

void ell_io_attr_set_id(ell_io_attr  *io, char *ident);

void ell_io_attr_set_parent(ell_io_attr  *io, char *ident);


#ifdef __cplusplus
}
#endif


#endif //__ELLIPTICS__DNET_ATTR_H
