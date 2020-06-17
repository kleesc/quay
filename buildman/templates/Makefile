all: fcct-gen

template:
	python test-cloudconfig.py  > test-cloudconfig.yaml

fcct-gen: template
	fcct test-cloudconfig.yaml -o test-cloudconfig.ign
