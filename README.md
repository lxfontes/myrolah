# MySQL Role Checker

Checks if a node is master or slave, returning HTTP 200 if all is good.

## Why ?

- Not going to use Galera yet
- Mysql-MHA is great, but corosync/pacemaker across DCs is not a good idea
- Running a local/centralized HAProxy doesn't hurt (search for airbnb's synapse/nerve combo)

## Installation

    go get github.com/lxfontes/myrolah

    $ myrolah  -h
    Usage of myrolah:
    -lag=30: Slave Lag
    -master=":7555": Master HTTP Check Port
    -slave=":7556": Slave HTTP Check Port
    -url="root:@tcp(127.0.0.1:3306)/information_schema": DB URL

## Checks

### Am I the master ?

A node will be classified as master if:
- MySQL is up
- It doesn't have any slave config (blank show slave status)
- Global `read_only = OFF`

### Am I a slave ?
- MySQL is up
- Global `read_only = ON`
- Slave configuration is present (non-empty show slave status)
- Slave IO Thread is running
- Slave SQL Thread is running
- Slave is not lagging behind Master by X seconds

## HAProxy

The trick is to setup a different port for healthchecks via `check port`

    server db1 10.0.0.1:3306 check port 7556 inter 5s rise 2 fall 2 downinter 2s fastinter 1s

Check the sample haproxy.conf for a full example.


### Flipping master / slave

Make sure to kill all connections on old-master when promoting a slave to master or you will get a lot of 'database is read-only errors'.

