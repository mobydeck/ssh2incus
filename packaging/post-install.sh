#!/bin/sh

APP="ssh2incus"

/bin/systemctl daemon-reload || true

echo "--------------------------------------------------------"
echo " ${APP} package has been successfully installed."
echo ""
echo " Please follow the next steps to start the software:"
echo ""
echo "    sudo systemctl enable ${APP}"
echo "    sudo systemctl start ${APP}"
echo "    - or -"
echo "    sudo systemctl enable --now ${APP}"
echo ""
echo " Configuration settings can be adjusted here:"
echo "    /etc/default/${APP}"
echo "    /etc/${APP}/config.yaml"
echo "    /etc/${APP}/create-config.yaml"
echo "--------------------------------------------------------"
