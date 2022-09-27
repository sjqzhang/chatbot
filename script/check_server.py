#!/usr/bin/env python
# -*- coding:utf8 -*-
import platform,socket,re,os
def command(cmd, timeout=3):
    pipe = os.popen('{ ' + cmd + '; } 2>&1', 'r')
    text = pipe.read()
    sts = pipe.close()
    if sts is None: sts = 0
    if text[-1:] == '\n': text = text[:-1]
    return sts, text

def is_alive(port,address='127.0.0.1'):
    port=int(port)
    import socket
    s = socket.socket()
    print "Attempting to connect to %s on port %s" % (address, port)
    try:
        s.settimeout(5)
        s.connect((address, port))
        print "Connected to %s on port %s" % (address, port)
        return True
    except socket.error, e:
        print "Connection to %s on port %s failed: %s" % (address, port, e)
        return False
    finally:
        try:
            s.close()
        except Exception as er:
            pass


if not is_alive('8080'):
    cmd='cd /root/chatbot ; ./chatserver /dev/null 2>&1 & '
    os.system(cmd)
