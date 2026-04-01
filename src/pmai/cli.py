try:
    from .cli_main import main
except ImportError:
    from cli_main import main


if __name__ == "__main__":
    main()
