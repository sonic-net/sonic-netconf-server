#
# Software Name: sonic-netconf-server
# SPDX-FileCopyrightText: Copyright (c) Orange SA
# SPDX-License-Identifier: Apache 2.0
# 
# This software is distributed under the Apache 2.0 licence,
# the text of which is available at https:#opensource.org/license/apache-2-0/
# or see the "LICENSE" file for more details.
# 
# Authors: hossam4.hassan@orange.com, abdelmuhaimen.seaudi@orange.com
# Software description: RFC compliant NETCONF server implementation for SONiC
#


import optparse
import sys
import chevron
from pyang import plugin

def pyang_plugin_init():
    plugin.register_plugin(RPCsGenPlugin())

class RPCsGenPlugin(plugin.PyangPlugin):

    rpcs = []

    def add_output_format(self, fmts):
        self.multiple_modules = True
        fmts['rpcs'] = self

    def add_opts(self, optparser):
        optlist = []
        g = optparser.add_option_group("RPCsGenPlugin options")
        g.add_options(optlist)

    def emit(self, ctx, modules, fd):

        if ctx.opts.outdir is None:
            print("[Error]: Output folder is not mentioned")
            sys.exit(2)

        for module in modules:
            for child in module.i_children:
                self.walk_child(child)
        
        with open('../tools/templates/go-sets.mustache', 'r') as f:
            stuff = chevron.render(f, {
                'values' : self.rpcs,
                'name' : 'Rpcs',
                'type' : 'string',
            })  

        stream = open(ctx.opts.outdir + "/keys.go", 'w+')
        # stream = open(ctx.opts.outdir + "/" + ctx.opts.type + '.go', 'w+')
        stream.write(stuff)
        stream.close()

        
    def walk_child(self, child):
        if child.keyword == "rpc":
            rpcParent = child.parent.__str__().split(" ")[1]
            rpcName = child.__str__().split(" ")[1]
            self.rpcs.append(rpcParent + ":" + rpcName)
