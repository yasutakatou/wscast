from ipykernel.kernelbase import Kernel
import pexpect
from time import sleep
import sys

# expect_list = [r".*(([1-9]?[0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([1-9]?[0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5]):([1-9][0-9]{3}|[1-9][0-9]{2}|[1-9][0-9]{1}).*", r".*>>>.*"]

expect_list = ["- - - - - -"]

p = pexpect.spawn("/usr/local/bin/wscast")

class EchoKernel(Kernel):
    implementation = 'Echo'
    implementation_version = '1.0'
    language = 'no-op'
    language_version = '0.1'
    language_info = {
        'name': 'Any text',
        'mimetype': 'text/plain',
        'file_extension': '.txt',
    }
    banner = "Echo kernel - as useful as a parrot"

    def do_execute(self, code, silent, store_history=True, user_expressions=None,
                   allow_stdin=False):
        if not silent:
            p.send(code + '\n')
            sleep(1)
            p.expect('.*- - - - - -.*')
            hstring = p.after.decode(encoding='utf-8', errors='replace')
            stream_content = {'name': 'stdout', 'text': hstring }
            self.send_response(self.iopub_socket, 'stream', stream_content)

        return {'status': 'ok',
                # The base class increments the execution count
                'execution_count': self.execution_count,
                'payload': [],
                'user_expressions': {},
               }
