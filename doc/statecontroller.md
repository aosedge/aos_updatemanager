# State Controller

State Controller (SC) is used to extend and customize system upgrade functionality. The update handler (UH) notifies SC when new upgrade or revert request is received. SC may accept, reject or delay the system upgrade. UH notifies SC when all system modules are updated or reverted. SC may accept, reject, delay the upgrade or revert at this stage as well. SC may decide to do not finish the upgrade or revert procedure but rather to postpone it till next UM start. SC should provide the installed image version.

SC is optional and can be disabled. If SC is enabled, UH should:
* get current installed image version from SC;
* notify SC about upgrade start by calling appropriate API;
* notify SC when all modules are successfully upgraded by calling appropriate API.

```mermaid
sequenceDiagram
    participant SM
    participant UH
    participant SC
    participant Module

    SM ->> UH: System Upgrade V2
    UH ->> SC: Get System Version
    SC ->> UH: System Version V1
    UH ->> SC: Upgrade V2
    SC ->> UH: Accept
    loop Every module
        UH ->>+ Module: Upgrade
        Module ->>- UH: OK
    end
    UH ->> SC: Upgrade Finished V2
    SC ->> UH: Accept
    UH ->> SM: Upgrade success
```

SC can delay the system upgrade for example if it performs some system check etc. And upgrade is not possible at the moment.

```mermaid
sequenceDiagram
    participant SM
    participant UH
    participant SC
    participant Module

    Activate SC
    SM ->> UH: System Upgrade V2
    UH ->> SC: Get System Version
    SC ->> UH: System Version V1
    UH ->> SC: Upgrade V2
    Note right of SC: System check
    SC ->> UH: Accept
    Deactivate SC

    loop Every module
        UH ->>+ Module: Upgrade
        Module ->>- UH: OK
    end
    UH ->> SC: Upgrade Finished V2
    SC ->> UH: Accept
    UH ->> SM: Upgrade success
```

SC can reject the system upgrade due to some serious system error, for example. In this case failed upgrade status will be returned to SM:

```mermaid
sequenceDiagram
    participant SM
    participant UH
    participant SC
    participant Module

    SM ->> UH: System Upgrade V2
    UH ->> SC: Get System Version
    SC ->> UH: System Version V1
    UH ->> SC: Upgrade V2
    Note right of SC: System error
    SC ->> UH: Reject
    UH ->> SM: Upgrade failed
```

If system reboot etc. is required after success upgrade, SC can postpone sending upgrade status till next reboot:

```mermaid
sequenceDiagram
    participant SM
    participant UH
    participant SC
    participant Module

    SM ->> UH: System Upgrade V2
    UH ->> SC: Get System Version
    SC ->> UH: System Version V1
    UH ->> SC: Upgrade V2
    SC ->> UH: Accept
    loop Every module
        UH ->>+ Module: Upgrade
        Module ->>- UH: OK
    end
    UH ->> SC: Upgrade Finished V2
    SC ->> UH: Postpone
    Note over SM, Module: System reboot
    UH ->> SC: Upgrade Finished V2
    SC ->> UH: Accept
    UH ->> SM: Upgrade success
```
