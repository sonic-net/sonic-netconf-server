import optparse
import sys
import chevron
from pyang import plugin

def pyang_plugin_init():
    plugin.register_plugin(ListKeysGenPlugin())

class ListKeysGenPlugin(plugin.PyangPlugin):

    result = {}

    def add_output_format(self, fmts):
        self.multiple_modules = True
        fmts['keys'] = self

    def add_opts(self, optparser):
        optlist = [
            optparse.make_option("--type",
                                 type="string",
                                 dest="type",
                                 help="Yang type"),
            optparse.make_option("--outdir",
                                 type="string",
                                 dest="outdir",
                                 help="Output directory for specs")
        ]
        g = optparser.add_option_group("ListKeysGenPlugin options")
        g.add_options(optlist)

    def emit(self, ctx, modules, fd):

        if ctx.opts.type is None:
            print("[Error]: Input type is not mentioned")
            sys.exit(2)
        
        if ctx.opts.outdir is None:
            print("[Error]: Output folder is not mentioned")
            sys.exit(2)

        for module in modules:
            for child in module.i_children:
                self.walk_child(child)
        
        paths = []

        for key in self.result:

            values = []

            for value in self.result[key]:
                values.append({
                    'value' : value
                })

            paths.append({
                'key' : key,
                'values' : values
            })

        with open('../tools/templates/netconf-keys-template.mustache', 'r') as f:
            stuff = chevron.render(f, {
                'paths' : paths,
                'type' : ctx.opts.type
            }) 

        stream = open(ctx.opts.outdir + "/" + ctx.opts.type + '.go', 'w+')
        stream.write(stuff)
        stream.close()

        
    def walk_child(self, child):
        if child.keyword == "list":
            self.result[self.get_full_path(child)] = self.get_list_keys(child)

        if hasattr(child, 'i_children'):
            for ch in child.i_children:
                self.walk_child(ch)

    def get_full_path(self, node):
        path = ""
        while True:
            path = "/" + node.arg + path
            if node.keyword == "module":
                arg1 = "/{}/{}".format(node.arg, node.arg)
                arg2 = "/{}:{}".format(node.arg, node.arg)
                path = path.replace(arg1, arg2)
                break
            node = node.parent
        return path

    def get_list_keys(self, list_node):
        keys = []
        for _, list_key in enumerate(list_node.i_key):
            keys.append(list_key.arg)
        return keys
